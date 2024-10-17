package stream

import (
	"context"
	"fmt"
	"sync"

	"cosmossdk.io/log"
	cmtquery "github.com/cometbft/cometbft/libs/pubsub/query"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	tmrpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	tmtypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/evmos/ethermint/rpc/backend"
	rpctypes "github.com/evmos/ethermint/rpc/types"
	ethermint "github.com/evmos/ethermint/types"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	"google.golang.org/grpc"
)

const (
	streamSubscriberName = "ethermint-json-rpc"
	subscribBufferSize   = 1024

	headerStreamSegmentSize = 128
	headerStreamCapacity    = 128 * 32
	txStreamSegmentSize     = 1024
	txStreamCapacity        = 1024 * 32
	logStreamSegmentSize    = 2048
	logStreamCapacity       = 2048 * 32
)

var (
	evmEvents = cmtquery.MustCompile(fmt.Sprintf("%s='%s' AND %s.%s='%s'",
		tmtypes.EventTypeKey,
		tmtypes.EventTx,
		sdk.EventTypeMessage,
		sdk.AttributeKeyModule, evmtypes.ModuleName)).String()
	blockEvents  = tmtypes.QueryForEvent(tmtypes.EventNewBlock).String()
	evmTxHashKey = fmt.Sprintf("%s.%s", evmtypes.TypeMsgEthereumTx, evmtypes.AttributeKeyEthereumTxHash)
)

type RPCHeader struct {
	EthHeader *ethtypes.Header
	Hash      common.Hash
}

type validatorAccountFunc func(
	ctx context.Context, in *evmtypes.QueryValidatorAccountRequest, opts ...grpc.CallOption,
) (*evmtypes.QueryValidatorAccountResponse, error)

// RPCStream provides data streams for newHeads, logs, and pendingTransactions.
type RPCStream struct {
	evtClient rpcclient.EventsClient
	logger    log.Logger
	clientCtx client.Context

	headerStream    *Stream[RPCHeader]
	pendingTxStream *Stream[common.Hash]
	logStream       *Stream[*ethtypes.Log]

	wg               sync.WaitGroup
	validatorAccount validatorAccountFunc
}

func NewRPCStreams(
	evtClient rpcclient.EventsClient,
	logger log.Logger,
	clientCtx client.Context,
	validatorAccount validatorAccountFunc,
) (*RPCStream, error) {
	s := &RPCStream{
		evtClient:        evtClient,
		logger:           logger,
		clientCtx:        clientCtx,
		headerStream:     NewStream[RPCHeader](headerStreamSegmentSize, headerStreamCapacity),
		pendingTxStream:  NewStream[common.Hash](txStreamSegmentSize, txStreamCapacity),
		logStream:        NewStream[*ethtypes.Log](logStreamSegmentSize, logStreamCapacity),
		validatorAccount: validatorAccount,
	}

	ctx := context.Background()

	chBlocks, err := s.evtClient.Subscribe(ctx, streamSubscriberName, blockEvents, subscribBufferSize)
	if err != nil {
		return nil, err
	}

	chLogs, err := s.evtClient.Subscribe(ctx, streamSubscriberName, evmEvents, subscribBufferSize)
	if err != nil {
		if err := s.evtClient.UnsubscribeAll(context.Background(), streamSubscriberName); err != nil {
			s.logger.Error("failed to unsubscribe", "err", err)
		}
		return nil, err
	}

	go s.start(&s.wg, chBlocks, chLogs)

	return s, nil
}

func (s *RPCStream) Close() error {
	if err := s.evtClient.UnsubscribeAll(context.Background(), streamSubscriberName); err != nil {
		return err
	}
	s.wg.Wait()
	return nil
}

func (s *RPCStream) HeaderStream() *Stream[RPCHeader] {
	return s.headerStream
}

func (s *RPCStream) PendingTxStream() *Stream[common.Hash] {
	return s.pendingTxStream
}

func (s *RPCStream) LogStream() *Stream[*ethtypes.Log] {
	return s.logStream
}

// ListenPendingTx is a callback passed to application to listen for pending transactions in CheckTx.
func (s *RPCStream) ListenPendingTx(hash common.Hash) {
	s.pendingTxStream.Add(hash)
}

func (s *RPCStream) start(
	wg *sync.WaitGroup,
	chBlocks <-chan coretypes.ResultEvent,
	chLogs <-chan coretypes.ResultEvent,
) {
	wg.Add(1)
	defer func() {
		wg.Done()
		if err := s.evtClient.UnsubscribeAll(context.Background(), streamSubscriberName); err != nil {
			s.logger.Error("failed to unsubscribe", "err", err)
		}
	}()

	for {
		select {
		case ev, ok := <-chBlocks:
			if !ok {
				chBlocks = nil
				break
			}

			data, ok := ev.Data.(tmtypes.EventDataNewBlock)
			if !ok {
				s.logger.Error("event data type mismatch", "type", fmt.Sprintf("%T", ev.Data))
				continue
			}

			baseFee := rpctypes.BaseFeeFromEvents(data.ResultFinalizeBlock.Events)
			res, err := s.validatorAccount(
				rpctypes.ContextWithHeight(data.Block.Height),
				&evmtypes.QueryValidatorAccountRequest{
					ConsAddress: sdk.ConsAddress(data.Block.Header.ProposerAddress).String(),
				},
			)
			if err != nil {
				s.logger.Error("failed to get validator account", "err", err)
				continue
			}
			validator, err := sdk.AccAddressFromBech32(res.AccountAddress)
			if err != nil {
				s.logger.Error("failed to convert validator account", "err", err)
				continue
			}
			height := data.Block.Header.Height
			ctx := rpctypes.ContextWithHeight(height)
			gasLimit, err := rpctypes.BlockMaxGasFromConsensusParams(ctx, s.clientCtx, height)
			if err != nil {
				s.logger.Error("failed to query consensus params", "error", err.Error())
				continue
			}
			sc, ok := s.clientCtx.Client.(tmrpcclient.SignClient)
			if !ok {
				fmt.Println("failed to fetch block result from Tendermint", "height", height, "error", "invalid rpc client")
				continue
			}
			blockRes, err := sc.BlockResults(ctx, &height)
			if err != nil {
				s.logger.Error("failed to fetch block result from Tendermint", "height", height, "error", err.Error())
				continue
			}
			gasUsed, err := backend.GetGasUsed(blockRes)
			if err != nil {
				s.logger.Error("failed to get gasUsed", "height", height, "error", err.Error())
				continue
			}
			bloom, err := backend.GetBlockBloom(blockRes)
			if err != nil {
				s.logger.Error("HeaderByHash BlockBloom failed", "height", height, "error", err.Error())
				continue
			}
			header := rpctypes.EthHeaderFromTendermint(data.Block.Header, bloom, baseFee, validator)
			limit, err := ethermint.SafeUint64(gasLimit)
			if err != nil {
				s.logger.Error("invalid gasLimit", "height", height, "error", err.Error())
				continue
			}
			header.GasLimit = limit
			header.GasUsed = gasUsed
			s.headerStream.Add(RPCHeader{EthHeader: header, Hash: common.BytesToHash(data.Block.Header.Hash())})
		case ev, ok := <-chLogs:
			if !ok {
				chLogs = nil
				break
			}

			if _, ok := ev.Events[evmTxHashKey]; !ok {
				// ignore transaction as it's not from the evm module
				continue
			}

			// get transaction result data
			dataTx, ok := ev.Data.(tmtypes.EventDataTx)
			if !ok {
				s.logger.Error("event data type mismatch", "type", fmt.Sprintf("%T", ev.Data))
				continue
			}
			height, err := ethermint.SafeUint64(dataTx.TxResult.Height)
			if err != nil {
				continue
			}
			txLogs, err := evmtypes.DecodeTxLogsFromEvents(dataTx.TxResult.Result.Data, dataTx.TxResult.Result.Events, height)
			if err != nil {
				s.logger.Error("fail to decode evm tx response", "error", err.Error())
				continue
			}

			s.logStream.Add(txLogs...)
		}

		if chBlocks == nil && chLogs == nil {
			break
		}
	}
}
