package precompiles

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransferkeeper "github.com/cosmos/ibc-go/v3/modules/apps/transfer/keeper"
	"github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	ibcchannelkeeper "github.com/cosmos/ibc-go/v3/modules/core/04-channel/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/evmos/ethermint/x/evm/statedb"
)

var (
	TransferMethod abi.Method
	QueryAckMethod abi.Method

	_ statedb.StatefulPrecompiledContract = (*IbcContract)(nil)
	_ statedb.JournalEntry                = ibcMessageChange{}
)

func init() {
	addressType, _ := abi.NewType("address", "", nil)
	stringType, _ := abi.NewType("string", "", nil)
	uint256Type, _ := abi.NewType("uint256", "", nil)
	boolType, _ := abi.NewType("bool", "", nil)
	TransferMethod = abi.NewMethod(
		"transfer", "transfer", abi.Function, "", false, false, abi.Arguments{abi.Argument{
			Name: "portId",
			Type: stringType,
		}, abi.Argument{
			Name: "channelId",
			Type: stringType,
		}, abi.Argument{
			Name: "sender",
			Type: addressType,
		}, abi.Argument{
			Name: "recipient",
			Type: addressType,
		}, abi.Argument{
			Name: "amount",
			Type: uint256Type,
		}, abi.Argument{
			Name: "srcDenom",
			Type: stringType,
		}},
		abi.Arguments{abi.Argument{
			Name: "sequence",
			Type: uint256Type,
		}},
	)
	QueryAckMethod = abi.NewMethod(
		"queryAck", "queryAck", abi.Function, "", false, false, abi.Arguments{abi.Argument{
			Name: "portId",
			Type: stringType,
		}, abi.Argument{
			Name: "channelId",
			Type: stringType,
		}, abi.Argument{
			Name: "sequence",
			Type: uint256Type,
		}},
		abi.Arguments{abi.Argument{
			Name: "status",
			Type: boolType,
		}},
	)
}

type IbcContract struct {
	ctx            sdk.Context
	channelKeeper  *ibcchannelkeeper.Keeper
	transferKeeper *ibctransferkeeper.Keeper
	msgs           []*types.MsgTransfer
}

func NewIbcContractCreator(channelKeeper *ibcchannelkeeper.Keeper, transferKeeper *ibctransferkeeper.Keeper) statedb.PrecompiledContractCreator {
	return func(ctx sdk.Context) statedb.StatefulPrecompiledContract {
		msgs := []*types.MsgTransfer{}
		return &IbcContract{ctx, channelKeeper, transferKeeper, msgs}
	}
}

// RequiredGas calculates the contract gas use
func (ic *IbcContract) RequiredGas(input []byte) uint64 {
	// TODO estimate required gas
	return 0
}

func (ic *IbcContract) Run(evm *vm.EVM, input []byte, caller common.Address, value *big.Int, readonly bool) ([]byte, error) {
	stateDB, ok := evm.StateDB.(ExtStateDB)
	if !ok {
		return nil, errors.New("not run in ethermint")
	}
	methodID := input[:4]
	fmt.Printf("IbcContract: %x\n", methodID)
	if bytes.Equal(methodID, TransferMethod.ID) {
		if readonly {
			return nil, errors.New("the method is not readonly")
		}
		args, err := TransferMethod.Inputs.Unpack(input[4:])
		if err != nil {
			return nil, errors.New("fail to unpack input arguments")
		}
		portId := args[0].(string)
		channelId := args[1].(string)
		sender := args[2].(common.Address)
		receiver := args[3].(common.Address)
		amount := args[4].(*big.Int)
		denom := args[5].(string)
		timeoutTimestamp := uint64(time.Now().UnixNano() + 60*1000000000)
		timeoutHeight := clienttypes.NewHeight(2, 100000)
		fmt.Printf(
			"TransferMethod portId: %s, channelId: %s, sender:%s, receiver: %s, amount: %s, denom: %s, timeoutTimestamp: %d, timeoutHeight: %s\n",
			portId, channelId, sender, receiver, amount.String(), denom, timeoutTimestamp, timeoutHeight,
		)
		token := sdk.NewCoin(denom, sdk.NewInt(amount.Int64()))
		msg := &types.MsgTransfer{
			SourcePort:       portId,
			SourceChannel:    channelId,
			Token:            token,
			Sender:           sender.String(),
			Receiver:         receiver.String(),
			TimeoutHeight:    timeoutHeight,
			TimeoutTimestamp: timeoutTimestamp,
		}
		ic.msgs = append(ic.msgs, msg)
		stateDB.AppendJournalEntry(ibcMessageChange{ic, caller, receiver, msg})
		sequence, found := ic.channelKeeper.GetNextSequenceSend(ic.ctx, portId, channelId)
		if !found {
			sequence = 0
		} else {
			sequence++
		}
		fmt.Printf("TransferMethod sequence: %d\n", sequence)
		return TransferMethod.Outputs.Pack(new(big.Int).SetUint64(sequence))
	} else if bytes.Equal(methodID, QueryAckMethod.ID) {
		args, err := QueryAckMethod.Inputs.Unpack(input[4:])
		if err != nil {
			return nil, errors.New("fail to unpack input arguments")
		}
		portId := args[0].(string)
		channelId := args[1].(string)
		sequence := args[2].(*big.Int)
		seq := sequence.Uint64()
		fmt.Printf("QueryAckMethod portId: %s, channelId: %s, sequence: %d\n", portId, channelId, seq)
		// ic.channelKeeper.GetPacketAcknowledgement(ctx, portId, channelId, seq)
		return nil, nil
	} else {
		return nil, errors.New("unknown method")
	}
}

func (ic *IbcContract) Commit(ctx sdk.Context) error {
	for _, msg := range ic.msgs {
		fmt.Printf("Commit: %+v\n", msg)
		// res, err := ic.transferKeeper.Transfer(ic.ctx.Context(), msg)
		src, err := sdk.AccAddressFromHex(strings.TrimPrefix(msg.Sender, "0x"))
		if err != nil {
			return err
		}
		if err := ic.transferKeeper.SendTransfer(
			ctx, msg.SourcePort, msg.SourceChannel, msg.Token, src, msg.Receiver, msg.TimeoutHeight, msg.TimeoutTimestamp,
		); err != nil {
			return err
		}
	}
	return nil
}

type ibcMessageChange struct {
	ic       *IbcContract
	caller   common.Address
	receiver common.Address
	msg      *types.MsgTransfer
}

func (ch ibcMessageChange) Revert(*statedb.StateDB) {
}

func (ch ibcMessageChange) Dirtied() *common.Address {
	return nil
}
