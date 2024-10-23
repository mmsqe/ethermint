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
package statedb

import (
	"fmt"
	"math/big"
	"sort"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/store/cachemulti"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

const StateDBContextKey = "statedb"

type EventConverter = func(sdk.Event) (*ethtypes.Log, error)

// revision is the identifier of a version of state.
// it consists of an auto-increment id and a journal index.
// it's safer to use than using journal index alone.
type revision struct {
	id           int
	journalIndex int
}

func Transfer(db vm.StateDB, sender, recipient common.Address, amount *big.Int) {
	db.(*StateDB).Transfer(sender, recipient, amount)
}

var _ vm.StateDB = &StateDB{}

// StateDB structs within the ethereum protocol are used to store anything
// within the merkle trie. StateDBs take care of caching and storing
// nested states. It's the general query interface to retrieve:
// * Contracts
// * Accounts
type StateDB struct {
	keeper Keeper
	// origCtx is the context passed in by the caller
	origCtx sdk.Context
	// ctx is a branched context on top of the caller context
	ctx sdk.Context
	// cacheMS caches the `ctx.MultiStore()` to avoid type assertions all the time, `ctx.MultiStore()` is not modified during the whole time,
	// which is evident by `ctx.WithMultiStore` is not called after statedb constructed.
	cacheMS cachemulti.Store

	// the action to commit native state, there are two cases:
	// if the parent store is not `cachemulti.Store`, we create a new one, and call `Write` to commit, this could only happen in unit tests.
	// if the parent store is already a `cachemulti.Store`, we branch it and call `Restore` to commit.
	commitMS func()

	// Journal of state modifications. This is the backbone of
	// Snapshot and RevertToSnapshot.
	journal        *journal
	validRevisions []revision
	nextRevisionID int

	stateObjects map[common.Address]*stateObject

	txConfig TxConfig

	// The refund counter, also used by state transitioning.
	refund uint64

	// Per-transaction logs
	logs []*ethtypes.Log

	// Per-transaction access list
	accessList *accessList

	// Transient storage
	transientStorage transientStorage

	// events emitted by native action
	nativeEvents sdk.Events

	// handle balances natively
	evmDenom string
	err      error
}

// New creates a new state from a given trie.
func New(ctx sdk.Context, keeper Keeper, txConfig TxConfig) *StateDB {
	return NewWithParams(ctx, keeper, txConfig, keeper.GetParams(ctx).EvmDenom)
}

func NewWithParams(ctx sdk.Context, keeper Keeper, txConfig TxConfig, evmDenom string) *StateDB {
	var (
		cacheMS  cachemulti.Store
		commitMS func()
	)
	if parentCacheMS, ok := ctx.MultiStore().(cachemulti.Store); ok {
		cacheMS = parentCacheMS.Clone()
		commitMS = func() { parentCacheMS.Restore(cacheMS) }
	} else {
		// in unit test, it could be run with a uncached multistore
		if cacheMS, ok = ctx.MultiStore().CacheWrap().(cachemulti.Store); !ok {
			panic("expect the CacheWrap result to be cachemulti.Store")
		}
		commitMS = cacheMS.Write
	}
	db := &StateDB{
		origCtx:      ctx,
		keeper:       keeper,
		cacheMS:      cacheMS,
		commitMS:     commitMS,
		stateObjects: make(map[common.Address]*stateObject),
		journal:      newJournal(),
		accessList:   newAccessList(),

		txConfig: txConfig,

		nativeEvents: sdk.Events{},
		evmDenom:     evmDenom,
	}
	db.ctx = ctx.WithValue(StateDBContextKey, db).WithMultiStore(cacheMS)
	return db
}

func (s *StateDB) NativeEvents() sdk.Events {
	return s.nativeEvents
}

// Keeper returns the underlying `Keeper`
func (s *StateDB) Keeper() Keeper {
	return s.keeper
}

// AddLog adds a log, called by evm.
func (s *StateDB) AddLog(log *ethtypes.Log) {
	s.journal.append(addLogChange{})

	log.TxIndex = s.txConfig.TxIndex
	log.Index = s.txConfig.LogIndex + uint(len(s.logs))
	s.logs = append(s.logs, log)
}

// Logs returns the logs of current transaction.
func (s *StateDB) Logs() []*ethtypes.Log {
	return s.logs
}

// AddRefund adds gas to the refund counter
func (s *StateDB) AddRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	s.refund += gas
}

// SubRefund removes gas from the refund counter.
// This method will panic if the refund counter goes below zero
func (s *StateDB) SubRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	if gas > s.refund {
		panic(fmt.Sprintf("Refund counter below zero (gas: %d > refund: %d)", gas, s.refund))
	}
	s.refund -= gas
}

// Exist reports whether the given account address exists in the state.
// Notably this also returns true for suicided accounts.
func (s *StateDB) Exist(addr common.Address) bool {
	return s.getStateObject(addr) != nil
}

// Empty returns whether the state object is either non-existent
// or empty according to the EIP161 specification (balance = nonce = code = 0)
func (s *StateDB) Empty(addr common.Address) bool {
	so := s.getStateObject(addr)
	if so == nil {
		return true
	}
	return so.empty() && s.GetBalance(addr).Sign() == 0
}

// GetBalance retrieves the balance from the given address or 0 if object not found
func (s *StateDB) GetBalance(addr common.Address) *big.Int {
	return s.keeper.GetBalance(s.ctx, sdk.AccAddress(addr.Bytes()), s.evmDenom)
}

// GetNonce returns the nonce of account, 0 if not exists.
func (s *StateDB) GetNonce(addr common.Address) uint64 {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Nonce()
	}

	return 0
}

// GetCode returns the code of account, nil if not exists.
func (s *StateDB) GetCode(addr common.Address) []byte {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.Code()
	}
	return nil
}

// GetCodeSize returns the code size of account.
func (s *StateDB) GetCodeSize(addr common.Address) int {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.CodeSize()
	}
	return 0
}

// GetCodeHash returns the code hash of account.
func (s *StateDB) GetCodeHash(addr common.Address) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return common.Hash{}
	}
	return common.BytesToHash(stateObject.CodeHash())
}

// GetState retrieves a value from the given account's storage trie.
func (s *StateDB) GetState(addr common.Address, hash common.Hash) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.GetState(hash)
	}
	return common.Hash{}
}

// GetCommittedState retrieves a value from the given account's committed storage trie.
func (s *StateDB) GetCommittedState(addr common.Address, hash common.Hash) common.Hash {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.GetCommittedState(hash)
	}
	return common.Hash{}
}

// GetRefund returns the current value of the refund counter.
func (s *StateDB) GetRefund() uint64 {
	return s.refund
}

// HasSuicided returns if the contract is suicided in current transaction.
func (s *StateDB) HasSuicided(addr common.Address) bool {
	stateObject := s.getStateObject(addr)
	if stateObject != nil {
		return stateObject.suicided
	}
	return false
}

// AddPreimage records a SHA3 preimage seen by the VM.
// AddPreimage performs a no-op since the EnablePreimageRecording flag is disabled
// on the vm.Config during state transitions. No store trie preimages are written
// to the database.
func (s *StateDB) AddPreimage(_ common.Hash, _ []byte) {}

// getStateObject retrieves a state object given by the address, returning nil if
// the object is not found.
func (s *StateDB) getStateObject(addr common.Address) *stateObject {
	// Prefer live objects if any is available
	if obj := s.stateObjects[addr]; obj != nil {
		return obj
	}
	// If no live objects are available, load it from keeper
	account := s.keeper.GetAccount(s.ctx, addr)
	if account == nil {
		return nil
	}
	// Insert into the live set
	obj := newObject(s, addr, account)
	s.setStateObject(obj)
	return obj
}

// getOrNewStateObject retrieves a state object or create a new state object if nil.
func (s *StateDB) getOrNewStateObject(addr common.Address) *stateObject {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		stateObject = s.createObject(addr)
	}
	return stateObject
}

// createObject creates a new state object. If there is an existing account with
// the given address, it is overwritten and returned as the second return value.
func (s *StateDB) createObject(addr common.Address) *stateObject {
	prev := s.getStateObject(addr)

	newobj := newObject(s, addr, nil)
	if prev == nil {
		s.journal.append(createObjectChange{account: &addr})
	} else {
		s.journal.append(resetObjectChange{prev: prev})
	}
	s.setStateObject(newobj)
	return newobj
}

// CreateAccount explicitly creates a state object. If a state object with the address
// already exists the balance is carried over to the new account.
//
// CreateAccount is called during the EVM CREATE operation. The situation might arise that
// a contract does the following:
//
// 1. sends funds to sha(account ++ (nonce + 1))
// 2. tx_create(sha(account ++ nonce)) (note that this gets the address of 1)
//
// Carrying over the balance ensures that Ether doesn't disappear.
func (s *StateDB) CreateAccount(addr common.Address) {
	s.createObject(addr)
}

// ForEachStorage iterate the contract storage, the iteration order is not defined.
func (s *StateDB) ForEachStorage(addr common.Address, cb func(key, value common.Hash) bool) error {
	so := s.getStateObject(addr)
	if so == nil {
		return nil
	}
	s.keeper.ForEachStorage(s.ctx, addr, func(key, value common.Hash) bool {
		if value, dirty := so.dirtyStorage[key]; dirty {
			return cb(key, value)
		}
		if len(value) > 0 {
			return cb(key, value)
		}
		return true
	})
	return nil
}

func (s *StateDB) setStateObject(object *stateObject) {
	s.stateObjects[object.Address()] = object
}

func (s *StateDB) snapshotNativeState() cachemulti.Store {
	return s.cacheMS.Clone()
}

func (s *StateDB) revertNativeStateToSnapshot(ms cachemulti.Store) {
	s.cacheMS.Restore(ms)
}

// ExecuteNativeAction executes native action in isolate,
// the writes will be revert when either the native action itself fail
// or the wrapping message call reverted.
func (s *StateDB) ExecuteNativeAction(contract common.Address, converter EventConverter, action func(ctx sdk.Context) error) error {
	snapshot := s.snapshotNativeState()
	eventManager := sdk.NewEventManager()

	if err := action(s.ctx.WithEventManager(eventManager)); err != nil {
		s.revertNativeStateToSnapshot(snapshot)
		return err
	}

	events := eventManager.Events()
	s.emitNativeEvents(contract, converter, events)
	s.nativeEvents = s.nativeEvents.AppendEvents(events)
	s.journal.append(nativeChange{snapshot: snapshot, events: len(events)})
	return nil
}

// Context returns the current context for query native state in precompiles.
func (s *StateDB) Context() sdk.Context {
	return s.ctx
}

/*
 * SETTERS
 */

// Transfer from one account to another
func (s *StateDB) Transfer(sender, recipient common.Address, amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	if amount.Sign() < 0 {
		panic("negative amount")
	}

	coins := sdk.NewCoins(sdk.NewCoin(s.evmDenom, sdkmath.NewIntFromBigIntMut(amount)))
	senderAddr := sdk.AccAddress(sender.Bytes())
	recipientAddr := sdk.AccAddress(recipient.Bytes())
	if err := s.ExecuteNativeAction(common.Address{}, nil, func(ctx sdk.Context) error {
		return s.keeper.Transfer(ctx, senderAddr, recipientAddr, coins)
	}); err != nil {
		s.err = err
	}
}

// AddBalance adds amount to the account associated with addr.
func (s *StateDB) AddBalance(addr common.Address, amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	if amount.Sign() < 0 {
		panic("negative amount")
	}
	coins := sdk.Coins{sdk.NewCoin(s.evmDenom, sdkmath.NewIntFromBigInt(amount))}
	if err := s.ExecuteNativeAction(common.Address{}, nil, func(ctx sdk.Context) error {
		return s.keeper.AddBalance(ctx, sdk.AccAddress(addr.Bytes()), coins)
	}); err != nil {
		s.err = err
	}
}

// SubBalance subtracts amount from the account associated with addr.
func (s *StateDB) SubBalance(addr common.Address, amount *big.Int) {
	if amount.Sign() == 0 {
		return
	}
	if amount.Sign() < 0 {
		panic("negative amount")
	}
	coins := sdk.Coins{sdk.NewCoin(s.evmDenom, sdkmath.NewIntFromBigInt(amount))}
	if err := s.ExecuteNativeAction(common.Address{}, nil, func(ctx sdk.Context) error {
		return s.keeper.SubBalance(ctx, sdk.AccAddress(addr.Bytes()), coins)
	}); err != nil {
		s.err = err
	}
}

// SetBalance is called by state override
func (s *StateDB) SetBalance(addr common.Address, amount *big.Int) {
	if err := s.ExecuteNativeAction(common.Address{}, nil, func(ctx sdk.Context) error {
		return s.keeper.SetBalance(ctx, addr, amount, s.evmDenom)
	}); err != nil {
		s.err = err
	}
}

// SetNonce sets the nonce of account.
func (s *StateDB) SetNonce(addr common.Address, nonce uint64) {
	stateObject := s.getOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetNonce(nonce)
	}
}

// SetCode sets the code of account.
func (s *StateDB) SetCode(addr common.Address, code []byte) {
	stateObject := s.getOrNewStateObject(addr)
	if stateObject != nil {
		stateObject.SetCode(crypto.Keccak256Hash(code), code)
	}
}

// SetState sets the contract state.
func (s *StateDB) SetState(addr common.Address, key, value common.Hash) {
	stateObject := s.getOrNewStateObject(addr)
	stateObject.SetState(key, value)
}

// SetStorage replaces the entire storage for the specified account with given
// storage. This function should only be used for debugging and the mutations
// must be discarded afterwards.
func (s *StateDB) SetStorage(addr common.Address, storage Storage) {
	stateObject := s.getOrNewStateObject(addr)
	stateObject.SetStorage(storage)
}

// Suicide marks the given account as suicided.
// This clears the account balance.
//
// The account's state object is still available until the state is committed,
// getStateObject will return a non-nil account after Suicide.
func (s *StateDB) Suicide(addr common.Address) bool {
	stateObject := s.getStateObject(addr)
	if stateObject == nil {
		return false
	}
	s.journal.append(suicideChange{
		account: &addr,
		prev:    stateObject.suicided,
	})
	stateObject.markSuicided()

	// clear balance
	balance := s.GetBalance(addr)
	if balance.Sign() > 0 {
		s.SubBalance(addr, balance)
	}

	return true
}

// SetTransientState sets transient storage for a given account. It
// adds the change to the journal so that it can be rolled back
// to its previous value if there is a revert.
func (s *StateDB) SetTransientState(addr common.Address, key, value common.Hash) {
	prev := s.GetTransientState(addr, key)
	if prev == value {
		return
	}

	s.journal.append(transientStorageChange{
		account:  &addr,
		key:      key,
		prevalue: prev,
	})

	s.setTransientState(addr, key, value)
}

// setTransientState is a lower level setter for transient storage. It
// is called during a revert to prevent modifications to the journal.
func (s *StateDB) setTransientState(addr common.Address, key, value common.Hash) {
	s.transientStorage.Set(addr, key, value)
}

// GetTransientState gets transient storage for a given account.
func (s *StateDB) GetTransientState(addr common.Address, key common.Hash) common.Hash {
	return s.transientStorage.Get(addr, key)
}

// Prepare handles the preparatory steps for executing a state transition with.
// This method must be invoked before state transition.
//
// Berlin fork:
// - Add sender to access list (2929)
// - Add destination to access list (2929)
// - Add precompiles to access list (2929)
// - Add the contents of the optional tx access list (2930)
//
// Potential EIPs:
// - Reset access list (Berlin)
// - Add coinbase to access list (EIP-3651)
// - Reset transient storage (EIP-1153)
func (s *StateDB) Prepare(
	rules params.Rules,
	sender,
	coinbase common.Address,
	dst *common.Address,
	precompiles []common.Address,
	list ethtypes.AccessList,
) {
	if rules.IsBerlin {
		// Clear out any leftover from previous executions
		al := newAccessList()
		s.accessList = al

		al.AddAddress(sender)
		if dst != nil {
			al.AddAddress(*dst)
			// If it's a create-tx, the destination will be added inside evm.create
		}
		for _, addr := range precompiles {
			al.AddAddress(addr)
		}
		for _, el := range list {
			al.AddAddress(el.Address)
			for _, key := range el.StorageKeys {
				al.AddSlot(el.Address, key)
			}
		}
		if rules.IsShanghai { // EIP-3651: warm coinbase
			al.AddAddress(coinbase)
		}
	}
	// Reset transient storage at the beginning of transaction execution
	s.transientStorage = newTransientStorage()
}

// AddAddressToAccessList adds the given address to the access list
func (s *StateDB) AddAddressToAccessList(addr common.Address) {
	if s.accessList.AddAddress(addr) {
		s.journal.append(accessListAddAccountChange{&addr})
	}
}

// AddSlotToAccessList adds the given (address, slot)-tuple to the access list
func (s *StateDB) AddSlotToAccessList(addr common.Address, slot common.Hash) {
	addrMod, slotMod := s.accessList.AddSlot(addr, slot)
	if addrMod {
		// In practice, this should not happen, since there is no way to enter the
		// scope of 'address' without having the 'address' become already added
		// to the access list (via call-variant, create, etc).
		// Better safe than sorry, though
		s.journal.append(accessListAddAccountChange{&addr})
	}
	if slotMod {
		s.journal.append(accessListAddSlotChange{
			address: &addr,
			slot:    &slot,
		})
	}
}

// AddressInAccessList returns true if the given address is in the access list.
func (s *StateDB) AddressInAccessList(addr common.Address) bool {
	return s.accessList.ContainsAddress(addr)
}

// SlotInAccessList returns true if the given (address, slot)-tuple is in the access list.
func (s *StateDB) SlotInAccessList(addr common.Address, slot common.Hash) (addressPresent bool, slotPresent bool) {
	return s.accessList.Contains(addr, slot)
}

// Snapshot returns an identifier for the current revision of the state.
func (s *StateDB) Snapshot() int {
	id := s.nextRevisionID
	s.nextRevisionID++
	s.validRevisions = append(s.validRevisions, revision{id, s.journal.length()})
	return id
}

// RevertToSnapshot reverts all state changes made since the given revision.
func (s *StateDB) RevertToSnapshot(revid int) {
	// Find the snapshot in the stack of valid snapshots.
	idx := sort.Search(len(s.validRevisions), func(i int) bool {
		return s.validRevisions[i].id >= revid
	})
	if idx == len(s.validRevisions) || s.validRevisions[idx].id != revid {
		panic(fmt.Errorf("revision id %v cannot be reverted", revid))
	}
	snapshot := s.validRevisions[idx].journalIndex

	// Replay the journal to undo changes and remove invalidated snapshots
	s.journal.Revert(s, snapshot)
	s.validRevisions = s.validRevisions[:idx]
}

func (s *StateDB) Error() error {
	return s.err
}

// Commit writes the dirty states to keeper
// the StateDB object should be discarded after committed.
func (s *StateDB) Commit() error {
	// if there's any errors during the execution, abort
	if s.err != nil {
		return s.err
	}

	// commit the native cache store first,
	// the states managed by precompiles and the other part of StateDB must not overlap.
	// after this, should only use the `origCtx`.
	s.commitMS()
	if len(s.nativeEvents) > 0 {
		s.origCtx.EventManager().EmitEvents(s.nativeEvents)
	}

	for _, addr := range s.journal.sortedDirties() {
		obj := s.stateObjects[addr]
		if obj.suicided {
			if err := s.keeper.DeleteAccount(s.origCtx, obj.Address()); err != nil {
				return errorsmod.Wrap(err, "failed to delete account")
			}
		} else {
			codeDirty := obj.codeDirty()
			if codeDirty && obj.code != nil {
				s.keeper.SetCode(s.origCtx, obj.CodeHash(), obj.code)
			}
			if codeDirty || obj.nonceDirty() {
				if err := s.keeper.SetAccount(s.origCtx, obj.Address(), obj.account); err != nil {
					return errorsmod.Wrap(err, "failed to set account")
				}
			}
			for _, key := range obj.dirtyStorage.SortedKeys() {
				value := obj.dirtyStorage[key]
				// Skip noop changes, persist actual changes
				if value == obj.originStorage[key] {
					continue
				}
				s.keeper.SetState(s.origCtx, obj.Address(), key, value.Bytes())
			}
		}
	}
	return nil
}

func (s *StateDB) emitNativeEvents(contract common.Address, converter EventConverter, events []sdk.Event) {
	if converter == nil {
		return
	}

	if len(events) == 0 {
		return
	}

	for _, event := range events {
		log, err := converter(event)
		if err != nil {
			s.ctx.Logger().Error("failed to convert event", "err", err)
			continue
		}
		if log == nil {
			continue
		}

		log.Address = contract
		s.AddLog(log)
	}
}
