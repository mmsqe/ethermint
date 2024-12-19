package types

import (
	paramtypes "cosmossdk.io/x/params/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type (
	LegacyParams = paramtypes.ParamSet
	// Subspace defines an interface that implements the legacy Cosmos SDK x/params Subspace type.
	// NOTE: This is used solely for migration of the Cosmos SDK x/params managed parameters.
	Subspace interface {
		GetParamSetIfExists(ctx sdk.Context, ps LegacyParams)
	}
)
