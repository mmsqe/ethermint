package v4_test

import (
	"testing"

	coretesting "cosmossdk.io/core/testing"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/evmos/ethermint/encoding"
	v4 "github.com/evmos/ethermint/x/feemarket/migrations/v4"
	"github.com/evmos/ethermint/x/feemarket/types"
	"github.com/stretchr/testify/require"
)

type mockSubspace struct {
	ps types.Params
}

func newMockSubspaceEmpty() mockSubspace {
	return mockSubspace{}
}

func newMockSubspace(ps types.Params) mockSubspace {
	return mockSubspace{ps: ps}
}

func (ms mockSubspace) GetParamSetIfExists(ctx sdk.Context, ps types.LegacyParams) {
	*ps.(*types.Params) = ms.ps
}

func TestMigrate(t *testing.T) {
	encCfg := encoding.MakeConfig()
	cdc := encCfg.Codec

	storeKey := storetypes.NewKVStoreKey(types.ModuleName)
	tKey := storetypes.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(storeKey, tKey)
	kvStore := ctx.KVStore(storeKey)

	legacySubspaceEmpty := newMockSubspaceEmpty()
	env := runtime.NewEnvironment(runtime.NewKVStoreService(storeKey), coretesting.NewNopLogger())
	require.Error(t, v4.MigrateStore(ctx, env.KVStoreService, legacySubspaceEmpty, cdc))

	legacySubspace := newMockSubspace(types.DefaultParams())
	require.NoError(t, v4.MigrateStore(ctx, env.KVStoreService, legacySubspace, cdc))

	paramsBz := kvStore.Get(types.ParamsKey)
	var params types.Params
	cdc.MustUnmarshal(paramsBz, &params)

	require.Equal(t, params, legacySubspace.ps)
}
