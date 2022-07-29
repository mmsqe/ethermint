package precompiles

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransferkeeper "github.com/cosmos/ibc-go/v3/modules/apps/transfer/keeper"
	"github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	ibcchannelkeeper "github.com/cosmos/ibc-go/v3/modules/core/04-channel/keeper"
	ibcchanneltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/evmos/ethermint/x/evm/statedb"
)

var (
	TransferMethod     abi.Method
	HasCommitMethod    abi.Method
	QueryNextSeqMethod abi.Method

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
			Type: stringType,
		}, abi.Argument{
			Name: "amount",
			Type: uint256Type,
		}, abi.Argument{
			Name: "srcDenom",
			Type: stringType,
		}, abi.Argument{
			Name: "timeout",
			Type: uint256Type,
		}},
		abi.Arguments{abi.Argument{
			Name: "sequence",
			Type: uint256Type,
		}},
	)
	HasCommitMethod = abi.NewMethod(
		"hasCommit", "hasCommit", abi.Function, "", false, false, abi.Arguments{abi.Argument{
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
	QueryNextSeqMethod = abi.NewMethod(
		"queryNextSeq", "queryNextSeq", abi.Function, "", false, false, abi.Arguments{abi.Argument{
			Name: "portId",
			Type: stringType,
		}, abi.Argument{
			Name: "channelId",
			Type: stringType,
		}},
		abi.Arguments{abi.Argument{
			Name: "sequence",
			Type: uint256Type,
		}},
	)
}

type IbcContract struct {
	ctx            sdk.Context
	channelKeeper  *ibcchannelkeeper.Keeper
	transferKeeper *ibctransferkeeper.Keeper
	bankKeeper     types.BankKeeper
	msgs           []*types.MsgTransfer
	module         string
	ibcDenom       string
	ratio          *big.Int
}

func NewIbcContractCreator(channelKeeper *ibcchannelkeeper.Keeper, transferKeeper *ibctransferkeeper.Keeper, bankKeeper types.BankKeeper, module, ibcDenom string, ratio *big.Int) statedb.PrecompiledContractCreator {
	return func(ctx sdk.Context) statedb.StatefulPrecompiledContract {
		msgs := []*types.MsgTransfer{}
		return &IbcContract{ctx, channelKeeper, transferKeeper, bankKeeper, msgs, module, ibcDenom, ratio}
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
		receiver := args[3].(string)
		amount := args[4].(*big.Int)
		denom := args[5].(string)
		timeout := args[6].(*big.Int)
		timeoutTimestamp := timeout.Uint64()
		timeoutHeight := clienttypes.NewHeight(1, 1000)
		fmt.Printf(
			"TransferMethod portId: %s, channelId: %s, sender:%s, receiver: %s, amount: %s, denom: %s, timeoutTimestamp: %d, timeoutHeight: %s\n",
			portId, channelId, sender, receiver, amount.String(), denom, timeoutTimestamp, timeoutHeight,
		)
		token := sdk.NewCoin(denom, sdk.NewInt(amount.Int64()))
		src := sdk.AccAddress(common.HexToAddress(sender.String()).Bytes())
		if err != nil {
			return nil, err
		}
		msg := &types.MsgTransfer{
			SourcePort:       portId,
			SourceChannel:    channelId,
			Token:            token,
			Sender:           src.String(),
			Receiver:         receiver,
			TimeoutHeight:    timeoutHeight,
			TimeoutTimestamp: timeoutTimestamp,
		}
		ic.msgs = append(ic.msgs, msg)
		stateDB.AppendJournalEntry(ibcMessageChange{ic, caller, receiver, msg})
		sequence, _ := ic.channelKeeper.GetNextSequenceSend(ic.ctx, portId, channelId)
		status := ic.channelKeeper.HasPacketCommitment(ic.ctx, portId, channelId, sequence)
		fmt.Printf("TransferMethod sequence: %d, %+v\n", sequence, status)
		return TransferMethod.Outputs.Pack(new(big.Int).SetUint64(sequence))
	} else if bytes.Equal(methodID, HasCommitMethod.ID) {
		args, err := HasCommitMethod.Inputs.Unpack(input[4:])
		if err != nil {
			return nil, errors.New("fail to unpack input arguments")
		}
		portId := args[0].(string)
		channelId := args[1].(string)
		sequence := args[2].(*big.Int)
		seq := sequence.Uint64()
		fmt.Printf("HasCommitMethod portId: %s, channelId: %s, sequence: %d\n", portId, channelId, seq)
		status := ic.channelKeeper.HasPacketCommitment(ic.ctx, portId, channelId, seq)
		fmt.Printf("HasCommitMethod status: %+v\n", status)
		return HasCommitMethod.Outputs.Pack(status)
	} else if bytes.Equal(methodID, QueryNextSeqMethod.ID) {
		args, err := QueryNextSeqMethod.Inputs.Unpack(input[4:])
		if err != nil {
			return nil, errors.New("fail to unpack input arguments")
		}
		portId := args[0].(string)
		channelId := args[1].(string)
		fmt.Printf("QueryNextSeqMethod portId: %s, channelId: %s\n", portId, channelId)
		sequence, _ := ic.channelKeeper.GetNextSequenceSend(ic.ctx, portId, channelId)
		fmt.Printf("QueryNextSeqMethod sequence: %d\n", sequence)
		return QueryNextSeqMethod.Outputs.Pack(new(big.Int).SetUint64(sequence))
	} else {
		return nil, errors.New("unknown method")
	}
}

func (ic *IbcContract) Commit(ctx sdk.Context) error {
	goCtx := sdk.WrapSDKContext(ic.ctx)
	for _, msg := range ic.msgs {
		acc, err := sdk.AccAddressFromBech32(msg.Sender)
		if err != nil {
			return err
		}
		c := msg.Token
		amount8decRem := c.Amount.Mod(sdk.NewIntFromBigInt(ic.ratio))
		amountToBurn := c.Amount.Sub(amount8decRem)
		if amountToBurn.IsZero() {
			// Amount too small
			continue
		}
		coins := sdk.NewCoins(sdk.NewCoin(msg.Token.Denom, amountToBurn))
		// Send evm tokens to escrow address
		err = ic.bankKeeper.SendCoinsFromAccountToModule(ctx, acc, ic.module, coins)
		if err != nil {
			return err
		}
		// Burns the evm tokens
		if err := ic.bankKeeper.BurnCoins(
			ctx, ic.module, coins); err != nil {
			return err
		}

		// Transfer ibc tokens back to the user
		amount8dec := c.Amount.Quo(sdk.NewIntFromBigInt(ic.ratio))
		ibcCoin := sdk.NewCoin(ic.ibcDenom, amount8dec)
		if err := ic.bankKeeper.SendCoinsFromModuleToAccount(
			ctx, ic.module, acc, sdk.NewCoins(ibcCoin),
		); err != nil {
			return err
		}
		msg.Token = ibcCoin
		res, err := ic.transferKeeper.Transfer(goCtx, msg)
		if err != nil {
			if ibcchanneltypes.ErrPacketTimeout.Is(err) {
				// Send from escrow address to evm tokens
				err = ic.bankKeeper.SendCoinsFromAccountToModule(ctx, acc, ic.module, coins)
				if err != nil {
					return err
				}
				// Mint the evm tokens
				if err := ic.bankKeeper.MintCoins(
					ctx, ic.module, coins); err != nil {
					return err
				}
				return nil
			}
			fmt.Printf("Transfer res: %+v, %+v\n", res, err)
			return err
		}
	}
	return nil
}

type ibcMessageChange struct {
	ic       *IbcContract
	caller   common.Address
	receiver string
	msg      *types.MsgTransfer
}

func (ch ibcMessageChange) Revert(*statedb.StateDB) {
}

func (ch ibcMessageChange) Dirtied() *common.Address {
	return nil
}
