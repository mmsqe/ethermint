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
	"context"
	"math/big"

	"cosmossdk.io/core/appmodule"
	storetypes "cosmossdk.io/store/types"
	paramstypes "cosmossdk.io/x/params/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/evmos/ethermint/x/feemarket/types"
)

// KeyPrefixBaseFeeV1 TODO: Temporary will be removed with params refactor PR
var KeyPrefixBaseFeeV1 = []byte{2}

// Keeper grants access to the Fee Market module state.
type Keeper struct {
	appmodule.Environment

	// Protobuf codec
	cdc codec.BinaryCodec
	// Store key required for the Fee Market Prefix KVStore.
	storeKey storetypes.StoreKey
	// the address capable of executing a MsgUpdateParams message. Typically, this should be the x/gov module account.
	authority sdk.AccAddress
	// Legacy subspace
	ss paramstypes.Subspace
}

// NewKeeper generates new fee market module keeper
func NewKeeper(
	cdc codec.BinaryCodec,
	env appmodule.Environment,
	authority sdk.AccAddress,
	ss paramstypes.Subspace,
) Keeper {
	return Keeper{
		Environment: env,
		cdc:         cdc,
		authority:   authority,
		ss:          ss,
	}
}

// ----------------------------------------------------------------------------
// Parent Block Gas Used
// Required by EIP1559 base fee calculation.
// ----------------------------------------------------------------------------

// SetBlockGasWanted sets the block gas wanted to the store.
// CONTRACT: this should be only called during EndBlock.
func (k Keeper) SetBlockGasWanted(ctx context.Context, gas uint64) {
	gasBz := sdk.Uint64ToBigEndian(gas)
	k.KVStoreService.OpenKVStore(ctx).Set(types.KeyPrefixBlockGasWanted, gasBz)
}

// GetBlockGasWanted returns the last block gas wanted value from the store.
func (k Keeper) GetBlockGasWanted(ctx context.Context) uint64 {
	bz, err := k.KVStoreService.OpenKVStore(ctx).Get(types.KeyPrefixBlockGasWanted)
	if err != nil {
		panic(err)
	}
	if len(bz) == 0 {
		return 0
	}
	return sdk.BigEndianToUint64(bz)
}

// GetBaseFeeV1 get the base fee from v1 version of states.
// return nil if base fee is not enabled
// TODO: Figure out if this will be deleted ?
func (k Keeper) GetBaseFeeV1(ctx context.Context) *big.Int {
	bz, err := k.KVStoreService.OpenKVStore(ctx).Get(KeyPrefixBaseFeeV1)
	if err != nil {
		panic(err)
	}
	if len(bz) == 0 {
		return nil
	}
	return new(big.Int).SetBytes(bz)
}
