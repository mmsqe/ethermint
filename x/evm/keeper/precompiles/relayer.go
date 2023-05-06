package precompiles

import (
	"encoding/binary"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/gogo/protobuf/proto"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	clienttypes "github.com/cosmos/ibc-go/v5/modules/core/02-client/types"
	ibckeeper "github.com/cosmos/ibc-go/v5/modules/core/keeper"
	tmtypes "github.com/cosmos/ibc-go/v5/modules/light-clients/07-tendermint/types"
)

type RelayerContract struct {
	ctx       sdk.Context
	ibcKeeper ibckeeper.Keeper
	stateDB   ExtStateDB
}

func NewRelayerContract(ctx sdk.Context, ibcKeeper ibckeeper.Keeper, stateDB ExtStateDB) StatefulPrecompiledContract {
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
)

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
		var msg clienttypes.MsgCreateClient
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgCreateClient")
		}
		var clientState tmtypes.ClientState
		if err := proto.Unmarshal(msg.ClientState.Value, &clientState); err != nil {
			return nil, errors.New("fail to Unmarshal ClientState")
		}
		msg.ClientState, err = codectypes.NewAnyWithValue(&clientState)
		if err != nil {
			return nil, err
		}
		var consensusState tmtypes.ConsensusState
		if err := proto.Unmarshal(msg.ConsensusState.Value, &consensusState); err != nil {
			return nil, errors.New("fail to Unmarshal ConsensusState")
		}
		msg.ConsensusState, err = codectypes.NewAnyWithValue(&consensusState)
		if err != nil {
			return nil, err
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.CreateClient(ctx, &msg)
			return err
		})
	case prefixUpdateClient:
		var msg clienttypes.MsgUpdateClient
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgUpdateClient")
		}
		var header tmtypes.Header
		if err := proto.Unmarshal(msg.Header.Value, &header); err != nil {
			return nil, errors.New("fail to Unmarshal Header")
		}
		msg.Header, err = codectypes.NewAnyWithValue(&header)
		if err != nil {
			return nil, err
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.UpdateClient(ctx, &msg)
			return err
		})
	case prefixUpgradeClient:
		var msg clienttypes.MsgUpgradeClient
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgUpgradeClient")
		}
		var clientState tmtypes.ClientState
		if err := proto.Unmarshal(msg.ClientState.Value, &clientState); err != nil {
			return nil, errors.New("fail to Unmarshal ClientState")
		}
		msg.ClientState, err = codectypes.NewAnyWithValue(&clientState)
		if err != nil {
			return nil, err
		}
		var consensusState tmtypes.ConsensusState
		if err := proto.Unmarshal(msg.ConsensusState.Value, &consensusState); err != nil {
			return nil, errors.New("fail to Unmarshal ConsensusState")
		}
		msg.ConsensusState, err = codectypes.NewAnyWithValue(&consensusState)
		if err != nil {
			return nil, err
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.UpgradeClient(ctx, &msg)
			return err
		})
	case prefixSubmitMisbehaviour:
		var msg clienttypes.MsgSubmitMisbehaviour
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgSubmitMisbehaviour")
		}
		var misbehaviour tmtypes.Misbehaviour
		if err := proto.Unmarshal(msg.Misbehaviour.Value, &misbehaviour); err != nil {
			return nil, errors.New("fail to Unmarshal Misbehaviour")
		}
		msg.Misbehaviour, err = codectypes.NewAnyWithValue(&misbehaviour)
		if err != nil {
			return nil, err
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.SubmitMisbehaviour(ctx, &msg)
			return err
		})
	default:
		return nil, errors.New("unknown method")
	}
	return nil, err
}
