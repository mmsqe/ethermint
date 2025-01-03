package v4

import (
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"

	v0types "github.com/evmos/ethermint/x/evm/migrations/v0/types"
	v4types "github.com/evmos/ethermint/x/evm/migrations/v4/types"
	"github.com/evmos/ethermint/x/evm/types"
)

// MigrateStore migrates the x/evm module state from the consensus version 3 to
// version 4. Specifically, it takes the parameters that are currently stored
// and managed by the Cosmos SDK params module and stores them directly into the x/evm module state.
func MigrateStore(
	ctx sdk.Context,
	storeService corestore.KVStoreService,
	legacySubspace types.Subspace,
	cdc codec.BinaryCodec,
) error {
	var params v0types.V0Params
	legacySubspace.GetParamSetIfExists(ctx, &params)
	if err := params.Validate(); err != nil {
		return err
	}
	chainCfgBz := cdc.MustMarshal(&params.ChainConfig)
	extraEIPsBz := cdc.MustMarshal(&v4types.ExtraEIPs{
		EIPs: params.ExtraEIPs,
	})
	store := runtime.KVStoreAdapter(storeService.OpenKVStore(ctx))
	store.Set(v0types.ParamStoreKeyEVMDenom, []byte(params.EvmDenom))
	store.Set(v0types.ParamStoreKeyExtraEIPs, extraEIPsBz)
	store.Set(v0types.ParamStoreKeyChainConfig, chainCfgBz)
	if params.AllowUnprotectedTxs {
		store.Set(v0types.ParamStoreKeyAllowUnprotectedTxs, []byte{0x01})
	}
	if params.EnableCall {
		store.Set(v0types.ParamStoreKeyEnableCall, []byte{0x01})
	}
	if params.EnableCreate {
		store.Set(v0types.ParamStoreKeyEnableCreate, []byte{0x01})
	}
	return nil
}
