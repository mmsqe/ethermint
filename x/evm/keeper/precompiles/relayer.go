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
	case prefixConnectionOpenInit:
		var msg conntypes.MsgConnectionOpenInit
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgConnectionOpenInit")
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.ConnectionOpenInit(ctx, &msg)
			return err
		})
	case prefixConnectionOpenTry:
		var msg conntypes.MsgConnectionOpenTry
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgConnectionOpenTry")
		}
		var clientState tmtypes.ClientState
		if err := proto.Unmarshal(msg.ClientState.Value, &clientState); err != nil {
			return nil, errors.New("fail to Unmarshal ClientState")
		}
		msg.ClientState, err = codectypes.NewAnyWithValue(&clientState)
		if err != nil {
			return nil, err
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.ConnectionOpenTry(ctx, &msg)
			return err
		})
	case prefixConnectionOpenAck:
		var msg conntypes.MsgConnectionOpenAck
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgConnectionOpenAck")
		}
		var clientState tmtypes.ClientState
		if err := proto.Unmarshal(msg.ClientState.Value, &clientState); err != nil {
			return nil, errors.New("fail to Unmarshal ClientState")
		}
		msg.ClientState, err = codectypes.NewAnyWithValue(&clientState)
		if err != nil {
			return nil, err
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.ConnectionOpenAck(ctx, &msg)
			return err
		})
	case prefixConnectionOpenConfirm:
		var msg conntypes.MsgConnectionOpenConfirm
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgConnectionOpenConfirm")
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.ConnectionOpenConfirm(ctx, &msg)
			return err
		})
	case prefixChannelOpenInit:
		var msg chantypes.MsgChannelOpenInit
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgChannelOpenInit")
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.ChannelOpenInit(ctx, &msg)
			return err
		})
	case prefixChannelOpenTry:
		var msg chantypes.MsgChannelOpenTry
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgChannelOpenTry")
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.ChannelOpenTry(ctx, &msg)
			return err
		})
	case prefixChannelOpenAck:
		var msg chantypes.MsgChannelOpenAck
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgChannelOpenAck")
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.ChannelOpenAck(ctx, &msg)
			return err
		})
	case prefixChannelOpenConfirm:
		var msg chantypes.MsgChannelOpenConfirm
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgChannelOpenConfirm")
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.ChannelOpenConfirm(ctx, &msg)
			return err
		})
	case prefixRecvPacket:
		var msg chantypes.MsgRecvPacket
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgRecvPacket")
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.RecvPacket(ctx, &msg)
			return err
		})
	case prefixAcknowledgement:
		var msg chantypes.MsgAcknowledgement
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgAcknowledgement")
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.Acknowledgement(ctx, &msg)
			return err
		})
	case prefixTimeout:
		var msg chantypes.MsgTimeout
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgTimeout")
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.Timeout(ctx, &msg)
			return err
		})
	case prefixTimeoutOnClose:
		var msg chantypes.MsgTimeoutOnClose
		if err := proto.Unmarshal(input, &msg); err != nil {
			return nil, errors.New("fail to Unmarshal MsgTimeoutOnClose")
		}
		err = bc.stateDB.ExecuteNativeAction(func(ctx sdk.Context) error {
			_, err := bc.ibcKeeper.TimeoutOnClose(ctx, &msg)
			return err
		})
	default:
		return nil, errors.New("unknown method")
	}
	return nil, err
}
