package precompiles

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/evmos/ethermint/rpc/backend"
	"github.com/gogo/protobuf/proto"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v5/modules/core/02-client/types"
	conntypes "github.com/cosmos/ibc-go/v5/modules/core/03-connection/types"
	chantypes "github.com/cosmos/ibc-go/v5/modules/core/04-channel/types"
	ibckeeper "github.com/cosmos/ibc-go/v5/modules/core/keeper"
	tmtypes "github.com/cosmos/ibc-go/v5/modules/light-clients/07-tendermint/types"
)

type RelayerContract struct {
	ctx       sdk.Context
	ibcKeeper *ibckeeper.Keeper
	stateDB   ExtStateDB
}

func NewRelayerContract(ctx sdk.Context, ibcKeeper *ibckeeper.Keeper, stateDB ExtStateDB) StatefulPrecompiledContract {
	return &RelayerContract{ctx, ibcKeeper, stateDB}
}

func (bc *RelayerContract) Address() common.Address {
	return common.BytesToAddress([]byte{101})
}

// RequiredGas calculates the contract gas use
func (bc *RelayerContract) RequiredGas(input []byte) uint64 {
	// TODO estimate required gas
	return 0
}

func (bc *RelayerContract) IsStateful() bool {
	return true
}

// prefix bytes for the relayer msg type
const (
	prefixSize4Bytes = 4
	// Client
	prefixCreateClient = iota + 1
	prefixUpdateClient
	prefixUpgradeClient
	prefixSubmitMisbehaviour
	// Connection
	prefixConnectionOpenInit
	prefixConnectionOpenTry
	prefixConnectionOpenAck
	prefixConnectionOpenConfirm
	// Channel
	prefixChannelOpenInit
	prefixChannelOpenTry
	prefixChannelOpenAck
	prefixChannelOpenConfirm
	prefixChannelCloseInit
	prefixChannelCloseConfirm
	prefixRecvPacket
	prefixAcknowledgement
	prefixTimeout
	prefixTimeoutOnClose
)

type MsgType interface {
	proto.Message
	*clienttypes.MsgCreateClient | *clienttypes.MsgUpdateClient | *clienttypes.MsgUpgradeClient | *clienttypes.MsgSubmitMisbehaviour |
		*conntypes.MsgConnectionOpenInit | *conntypes.MsgConnectionOpenTry | *conntypes.MsgConnectionOpenAck | *conntypes.MsgConnectionOpenConfirm |
		*chantypes.MsgChannelOpenInit | *chantypes.MsgChannelOpenTry | *chantypes.MsgChannelOpenAck | *chantypes.MsgChannelOpenConfirm | *chantypes.MsgRecvPacket | *chantypes.MsgAcknowledgement | *chantypes.MsgTimeout | *chantypes.MsgTimeoutOnClose
}

func unmarshalAndExec[T MsgType, U any](
	bc *RelayerContract,
	input []byte,
	msg T,
	callback func(context.Context, T) (U, error),
) error {
	if err := proto.Unmarshal(input, msg); err != nil {
		return fmt.Errorf("fail to Unmarshal %T", msg)
	}

	var anyMsg any = msg
	if clientMsg, ok := anyMsg.(clienttypes.ClientStateMsg); ok {
		clientState := new(tmtypes.ClientState)
		if err := proto.Unmarshal(clientMsg.GetClientState(), clientState); err != nil {
			return errors.New("fail to Unmarshal ClientState")
		}
		value, err := codectypes.NewAnyWithValue(clientState)
		if err != nil {
			return err
		}
		clientMsg.SetClientState(value)
	}
	if consensusMsg, ok := anyMsg.(clienttypes.ConsensusStateMsg); ok {
		consensusState := new(tmtypes.ConsensusState)
		if err := proto.Unmarshal(consensusMsg.GetConsensusState(), consensusState); err != nil {
			return errors.New("fail to Unmarshal ConsensusState")
		}
		value, err := codectypes.NewAnyWithValue(consensusState)
		if err != nil {
			return err
		}
		consensusMsg.SetConsensusState(value)
	}
	if headerMsg, ok := anyMsg.(clienttypes.HeaderMsg); ok {
		header := new(tmtypes.Header)
		if err := proto.Unmarshal(headerMsg.GetHeader(), header); err != nil {
			return errors.New("fail to Unmarshal Header")
		}
		value, err := codectypes.NewAnyWithValue(header)
		if err != nil {
			return err
		}
		headerMsg.SetHeader(value)
	}
	if misbehaviourMsg, ok := anyMsg.(clienttypes.MisbehaviourMsg); ok {
		misbehaviour := new(tmtypes.Misbehaviour)
		if err := proto.Unmarshal(misbehaviourMsg.GetMisbehaviour(), misbehaviour); err != nil {
			return errors.New("fail to Unmarshal Misbehaviour")
		}
		value, err := codectypes.NewAnyWithValue(misbehaviour)
		if err != nil {
			return err
		}
		misbehaviourMsg.SetMisbehaviour(value)
	}

	return bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
		startEventIdx := len(ctx.EventManager().Events())
		_, err := callback(ctx, msg)
		events := ctx.EventManager().Events()
		if len(events) > startEventIdx {
			txLogs, err := backend.AllLogsFromEvents(events[startEventIdx:].ToABCIEvents())
			if err != nil {
				return err
			}

			for _, logs := range txLogs {
				for _, log := range logs {
					bc.stateDB.AddLog(log)
				}
			}
		}
		return err
	})
}

func (bc *RelayerContract) Run(evm *vm.EVM, contract *vm.Contract, readonly bool) ([]byte, error) {
	if readonly {
		return nil, errors.New("the method is not readonly")
	}
	// parse input
	if len(contract.Input) < int(prefixSize4Bytes) {
		return nil, errors.New("data too short to contain prefix")
	}
	prefix := int(binary.LittleEndian.Uint32(contract.Input[:prefixSize4Bytes]))
	input := contract.Input[prefixSize4Bytes:]
	var err error
	switch prefix {
	case prefixCreateClient:
		err = unmarshalAndExec(bc, input, new(clienttypes.MsgCreateClient), bc.ibcKeeper.CreateClient)
	case prefixUpdateClient:
		err = unmarshalAndExec(bc, input, new(clienttypes.MsgUpdateClient), bc.ibcKeeper.UpdateClient)
	case prefixUpgradeClient:
		err = unmarshalAndExec(bc, input, new(clienttypes.MsgUpgradeClient), bc.ibcKeeper.UpgradeClient)
	case prefixSubmitMisbehaviour:
		err = unmarshalAndExec(bc, input, new(clienttypes.MsgSubmitMisbehaviour), bc.ibcKeeper.SubmitMisbehaviour)
	case prefixConnectionOpenInit:
		err = unmarshalAndExec(bc, input, new(conntypes.MsgConnectionOpenInit), bc.ibcKeeper.ConnectionOpenInit)
	case prefixConnectionOpenTry:
		err = unmarshalAndExec(bc, input, new(conntypes.MsgConnectionOpenTry), bc.ibcKeeper.ConnectionOpenTry)
	case prefixConnectionOpenAck:
		err = unmarshalAndExec(bc, input, new(conntypes.MsgConnectionOpenAck), bc.ibcKeeper.ConnectionOpenAck)
	case prefixConnectionOpenConfirm:
		err = unmarshalAndExec(bc, input, new(conntypes.MsgConnectionOpenConfirm), bc.ibcKeeper.ConnectionOpenConfirm)
	case prefixChannelOpenInit:
		err = unmarshalAndExec(bc, input, new(chantypes.MsgChannelOpenInit), bc.ibcKeeper.ChannelOpenInit)
	case prefixChannelOpenTry:
		err = unmarshalAndExec(bc, input, new(chantypes.MsgChannelOpenTry), bc.ibcKeeper.ChannelOpenTry)
	case prefixChannelOpenAck:
		err = unmarshalAndExec(bc, input, new(chantypes.MsgChannelOpenAck), bc.ibcKeeper.ChannelOpenAck)
	case prefixChannelOpenConfirm:
		err = unmarshalAndExec(bc, input, new(chantypes.MsgChannelOpenConfirm), bc.ibcKeeper.ChannelOpenConfirm)
	case prefixRecvPacket:
		err = unmarshalAndExec(bc, input, new(chantypes.MsgRecvPacket), bc.ibcKeeper.RecvPacket)
	case prefixAcknowledgement:
		err = unmarshalAndExec(bc, input, new(chantypes.MsgAcknowledgement), bc.ibcKeeper.Acknowledgement)
	case prefixTimeout:
		err = unmarshalAndExec(bc, input, new(chantypes.MsgTimeout), bc.ibcKeeper.Timeout)
	case prefixTimeoutOnClose:
		err = unmarshalAndExec(bc, input, new(chantypes.MsgTimeoutOnClose), bc.ibcKeeper.TimeoutOnClose)
	default:
		return nil, errors.New("unknown method")
	}
	return nil, err
}
