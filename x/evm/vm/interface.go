package vm

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// PrecompiledContracts defines a map of address -> precompiled contract
type PrecompiledContracts map[common.Address]vm.PrecompiledContract

type PrecompiledContractCreator func(
	sdk.Context,
	ExtStateDB,
	common.Address,
	*big.Int,
) StatefulPrecompiledContract

type StatefulPrecompiledContract interface {
	vm.PrecompiledContract
	// RunStateful(evm EVM, addr common.Address, input []byte, value *big.Int) (ret []byte, err error)
}

// ExtStateDB defines extra methods of statedb to support stateful precompiled contracts
type ExtStateDB interface {
	ExecuteNativeAction(action func(ctx sdk.Context) error) error
}

// EVM defines the interface for the Ethereum Virtual Machine used by the EVM module.
type EVM interface {
	Config() vm.Config
	Context() vm.BlockContext
	TxContext() vm.TxContext

	Reset(txCtx vm.TxContext, statedb vm.StateDB)
	Cancel()
	Cancelled() bool //nolint
	Interpreter() *vm.EVMInterpreter
	Call(caller vm.ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error)
	CallCode(caller vm.ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, err error)
	DelegateCall(caller vm.ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error)
	StaticCall(caller vm.ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, err error)
	Create(caller vm.ContractRef, code []byte, gas uint64, value *big.Int) (ret []byte, contractAddr common.Address, leftOverGas uint64, err error)
	Create2(
		caller vm.ContractRef,
		code []byte,
		gas uint64,
		endowment *big.Int,
		salt *uint256.Int) (
		ret []byte, contractAddr common.Address, leftOverGas uint64, err error,
	)
	ChainConfig() *params.ChainConfig

	ActivePrecompiles(rules params.Rules) []common.Address
	Precompile(addr common.Address) (vm.PrecompiledContract, bool)
	RunPrecompiledContract(
		p StatefulPrecompiledContract,
		input []byte,
		suppliedGas uint64) (
		ret []byte, remainingGas uint64, err error,
	)
}

// Constructor defines the function used to instantiate the EVM on
// each state transition.
type Constructor func(
	blockCtx vm.BlockContext,
	txCtx vm.TxContext,
	stateDB vm.StateDB,
	chainConfig *params.ChainConfig,
	config vm.Config,
	customPrecompiles PrecompiledContracts,
) EVM
