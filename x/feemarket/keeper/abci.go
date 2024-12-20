// Copyright 2021 Evmos Foundation
// This file is part of Evmos' Ethermint library.
//
// The Ethermint library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The Ethermint library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the Ethermint library. If not, see https://github.com/evmos/ethermint/blob/main/LICENSE
package keeper

import (
	"fmt"

	"github.com/evmos/ethermint/x/feemarket/types"

	"cosmossdk.io/core/event"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ethermint "github.com/evmos/ethermint/types"
)

// BeginBlock updates base fee
func (k *Keeper) BeginBlock(ctx sdk.Context) error {
	baseFee := k.CalculateBaseFee(ctx)

	// return immediately if base fee is nil
	if baseFee == nil {
		return nil
	}

	k.SetBaseFee(ctx, baseFee)

	defer func() {
		telemetry.SetGauge(float32(baseFee.Int64()), "feemarket", "base_fee")
	}()

	// Store current base fee in event
	k.EventService.EventManager(ctx).EmitKV(
		types.EventTypeFeeMarket,
		event.NewAttribute(types.AttributeKeyBaseFee, baseFee.String()),
	)
	return nil
}

// EndBlock update block gas wanted.
// The EVM end block logic doesn't update the validator set, thus it returns
// an empty slice.
func (k *Keeper) EndBlock(ctx sdk.Context) error {
	gasWanted := ctx.BlockGasWanted()
	gw, err := ethermint.SafeInt64(gasWanted)
	if err != nil {
		return err
	}
	gasUsed, err := ethermint.SafeInt64(ctx.BlockGasUsed())
	if err != nil {
		return err
	}

	// to prevent BaseFee manipulation we limit the gasWanted so that
	// gasWanted = max(gasWanted * MinGasMultiplier, gasUsed)
	// this will be keep BaseFee protected from un-penalized manipulation
	// more info here https://github.com/evmos/ethermint/pull/1105#discussion_r888798925
	minGasMultiplier := k.GetParams(ctx).MinGasMultiplier
	limitedGasWanted := sdkmath.LegacyNewDec(gw).Mul(minGasMultiplier)
	gasWanted = sdkmath.LegacyMaxDec(limitedGasWanted, sdkmath.LegacyNewDec(gasUsed)).TruncateInt().Uint64()
	k.SetBlockGasWanted(ctx, gasWanted)

	defer func() {
		telemetry.SetGauge(float32(gasWanted), "feemarket", "block_gas")
	}()

	k.EventService.EventManager(ctx).EmitKV(
		"block_gas",
		event.NewAttribute("height", fmt.Sprintf("%d", ctx.BlockHeight())),
		event.NewAttribute("amount", fmt.Sprintf("%d", gasWanted)),
	)
	return nil
}
