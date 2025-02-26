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
	"cosmossdk.io/core/event"
	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	paramstypes "cosmossdk.io/x/params/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	ethermint "github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm/statedb"
	"github.com/evmos/ethermint/x/evm/types"
)

// CustomContractFn defines a custom precompiled contract generator with ctx, rules and returns a precompiled contract.
type CustomContractFn func(sdk.Context, params.Rules) vm.PrecompiledContract

// Keeper grants access to the EVM module state and implements the go-ethereum StateDB interface.
type Keeper struct {
	appmodule.Environment
	// Protobuf codec
	cdc codec.Codec
	// Store key required for the EVM Prefix KVStore. It is required by:
	// - storing account's Storage State
	// - storing account's Code
	// - storing module parameters
	storeKey storetypes.StoreKey

	// key to access the object store, which is reset on every block during Commit
	objectKey storetypes.StoreKey

	// the address capable of executing a MsgUpdateParams message. Typically, this should be the x/gov module account.
	authority sdk.AccAddress
	// access to account state
	accountKeeper types.AccountKeeper
	// update balance and accounting operations with coins
	bankKeeper types.BankKeeper
	// access historical headers for EVM state transition execution
	stakingKeeper types.StakingKeeper
	// fetch EIP1559 base fee and parameters
	feeMarketKeeper types.FeeMarketKeeper

	// chain ID number obtained from the context's chain id
	eip155ChainID *big.Int

	// Tracer used to collect execution traces from the EVM transaction execution
	tracer string

	// EVM Hooks for tx post-processing
	hooks types.EvmHooks

	// Legacy subspace
	ss                paramstypes.Subspace
	customContractFns []CustomContractFn
}

// NewKeeper generates new evm module keeper
func NewKeeper(
	cdc codec.Codec,
	env appmodule.Environment,
	storeKey, objectKey storetypes.StoreKey,
	authority sdk.AccAddress,
	ak types.AccountKeeper,
	bankKeeper types.BankKeeper,
	sk types.StakingKeeper,
	fmk types.FeeMarketKeeper,
	tracer string,
	ss paramstypes.Subspace,
	customContractFns []CustomContractFn,
) *Keeper {
	// ensure evm module account is set
	if addr := ak.GetModuleAddress(types.ModuleName); addr == nil {
		panic("the EVM module account has not been set")
	}

	// NOTE: we pass in the parameter space to the CommitStateDB in order to use custom denominations for the EVM operations
	return &Keeper{
		Environment:       env,
		cdc:               cdc,
		authority:         authority,
		accountKeeper:     ak,
		bankKeeper:        bankKeeper,
		stakingKeeper:     sk,
		feeMarketKeeper:   fmk,
		storeKey:          storeKey,
		objectKey:         objectKey,
		tracer:            tracer,
		ss:                ss,
		customContractFns: customContractFns,
	}
}

// WithChainID sets the chain ID for the keeper by extracting it from the provided context
func (k *Keeper) WithChainID(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx) // mmsqe: https://github.com/cosmos/ibc-go/issues/5917
	k.WithChainIDString(sdkCtx.ChainID())
}

// WithChainIDString sets the chain ID for the keeper after parsing the provided string value
func (k *Keeper) WithChainIDString(value string) {
	chainID, err := ethermint.ParseChainID(value)
	if err != nil {
		panic(err)
	}

	if k.eip155ChainID != nil && k.eip155ChainID.Cmp(chainID) != 0 {
		panic("chain id already set")
	}

	k.eip155ChainID = chainID
}

// ChainID returns the EIP155 chain ID for the EVM context
func (k Keeper) ChainID() *big.Int {
	return k.eip155ChainID
}

// ----------------------------------------------------------------------------
// Block Bloom
// Required by Web3 API.
// ----------------------------------------------------------------------------

// EmitBlockBloomEvent emit block bloom events
func (k Keeper) EmitBlockBloomEvent(ctx context.Context, bloom []byte) {
	if err := k.EventService.EventManager(ctx).EmitKV(
		types.EventTypeBlockBloom,
		event.NewAttribute(types.AttributeKeyEthereumBloom, string(bloom)),
	); err != nil {
		// mmsqe
		k.Logger.Error("couldn't emit event", "error", err.Error())
	}
}

// GetAuthority returns the x/evm module authority address
func (k Keeper) GetAuthority() sdk.AccAddress {
	return k.authority
}

// ----------------------------------------------------------------------------
// Storage
// ----------------------------------------------------------------------------

// GetAccountStorage return state storage associated with an account
func (k Keeper) GetAccountStorage(ctx context.Context, address common.Address) types.Storage {
	storage := types.Storage{}

	k.ForEachStorage(ctx, address, func(key, value common.Hash) bool {
		storage = append(storage, types.NewState(key, value))
		return true
	})

	return storage
}

// ----------------------------------------------------------------------------
// Account
// ----------------------------------------------------------------------------

// SetHooks sets the hooks for the EVM module
// It should be called only once during initialization, it panic if called more than once.
func (k *Keeper) SetHooks(eh types.EvmHooks) *Keeper {
	if k.hooks != nil {
		panic("cannot set evm hooks twice")
	}

	k.hooks = eh
	return k
}

// PostTxProcessing delegate the call to the hooks. If no hook has been registered, this function returns with a `nil` error
func (k *Keeper) PostTxProcessing(ctx context.Context, msg *core.Message, receipt *ethtypes.Receipt) error {
	if k.hooks == nil {
		return nil
	}
	return k.hooks.PostTxProcessing(ctx, msg, receipt)
}

// Tracer return a default vm.Tracer based on current keeper state
func (k Keeper) Tracer(msg *core.Message, rules params.Rules) vm.EVMLogger {
	return types.NewTracer(k.tracer, msg, rules)
}

// GetAccount load nonce and codehash without balance,
// more efficient in cases where balance is not needed.
func (k *Keeper) GetAccount(ctx context.Context, addr common.Address) *statedb.Account {
	cosmosAddr := sdk.AccAddress(addr.Bytes())
	acct := k.accountKeeper.GetAccount(ctx, cosmosAddr)
	if acct == nil {
		return nil
	}
	return statedb.NewAccountFromSdkAccount(acct)
}

// GetAccountOrEmpty returns empty account if not exist, returns error if it's not `EthAccount`
func (k *Keeper) GetAccountOrEmpty(ctx context.Context, addr common.Address) statedb.Account {
	acct := k.GetAccount(ctx, addr)
	if acct != nil {
		return *acct
	}

	// empty account
	return statedb.Account{
		CodeHash: types.EmptyCodeHash,
	}
}

// GetNonce returns the sequence number of an account, returns 0 if not exists.
func (k *Keeper) GetNonce(ctx context.Context, addr common.Address) uint64 {
	cosmosAddr := sdk.AccAddress(addr.Bytes())
	acct := k.accountKeeper.GetAccount(ctx, cosmosAddr)
	if acct == nil {
		return 0
	}

	return acct.GetSequence()
}

// GetEVMDenomBalance returns the balance of evm denom
func (k *Keeper) GetEVMDenomBalance(ctx context.Context, addr common.Address) *big.Int {
	cosmosAddr := sdk.AccAddress(addr.Bytes())
	evmParams := k.GetParams(ctx)
	evmDenom := evmParams.GetEvmDenom()
	// if node is pruned, params is empty. Return invalid value
	if evmDenom == "" {
		return big.NewInt(-1)
	}
	return k.GetBalance(ctx, cosmosAddr, evmDenom)
}

// GetBalance load account's balance of specified denom
func (k *Keeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) *big.Int {
	return k.bankKeeper.GetBalance(ctx, addr, denom).Amount.BigInt()
}

// GetBaseFee returns current base fee, return values:
// - `nil`: london hardfork not enabled.
// - `0`: london hardfork enabled but feemarket is not enabled.
// - `n`: both london hardfork and feemarket are enabled.
func (k Keeper) GetBaseFee(ctx context.Context, ethCfg *params.ChainConfig) *big.Int {
	return k.getBaseFee(ctx, types.IsLondon(ethCfg, k.HeaderService.HeaderInfo(ctx).Height))
}

func (k Keeper) getBaseFee(ctx context.Context, london bool) *big.Int {
	if !london {
		return nil
	}
	baseFee := k.feeMarketKeeper.GetBaseFee(ctx)
	if baseFee == nil {
		// return 0 if feemarket not enabled.
		baseFee = big.NewInt(0)
	}
	return baseFee
}

// GetTransientGasUsed returns the gas used by current cosmos tx.
func (k Keeper) GetTransientGasUsed(ctx sdk.Context) uint64 {
	store := ctx.ObjectStore(k.objectKey)
	v := store.Get(types.ObjectGasUsedKey(ctx.TxIndex()))
	if v == nil {
		return 0
	}
	return v.(uint64)
}

// SetTransientGasUsed sets the gas used by current cosmos tx.
func (k Keeper) SetTransientGasUsed(ctx sdk.Context, gasUsed uint64) {
	store := ctx.ObjectStore(k.objectKey)
	store.Set(types.ObjectGasUsedKey(ctx.TxIndex()), gasUsed)
}

// AddTransientGasUsed accumulate gas used by each eth msgs included in current cosmos tx.
func (k Keeper) AddTransientGasUsed(ctx sdk.Context, gasUsed uint64) (uint64, error) {
	result := k.GetTransientGasUsed(ctx) + gasUsed
	if result < gasUsed {
		return 0, errorsmod.Wrap(types.ErrGasOverflow, "transient gas used")
	}
	k.SetTransientGasUsed(ctx, result)
	return result, nil
}

// SetHeaderHash stores the hash of the current block header in the store.
func (k Keeper) SetHeaderHash(ctx context.Context) {
	header := k.HeaderService.HeaderInfo(ctx)
	if len(header.Hash) == 0 {
		return
	}
	height, err := ethermint.SafeUint64(header.Height)
	if err != nil {
		panic(err)
	}
	store := k.KVStoreService.OpenKVStore(ctx)
	if err := store.Set(types.GetHeaderHashKey(height), header.Hash); err != nil {
		panic(err)
	}
}

// GetHeaderHash retrieves the hash of a block header from the store by height.
func (k Keeper) GetHeaderHash(ctx context.Context, height uint64) []byte {
	store := k.KVStoreService.OpenKVStore(ctx)
	bz, err := store.Get(types.GetHeaderHashKey(height))
	if err != nil {
		panic(err)
	}
	return bz
}

// DeleteHeaderHash removes the hash of a block header from the store by height
func (k Keeper) DeleteHeaderHash(ctx context.Context, height uint64) {
	store := k.KVStoreService.OpenKVStore(ctx)
	if err := store.Delete(types.GetHeaderHashKey(height)); err != nil {
		panic(err)
	}
}
