package ante_test

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"cosmossdk.io/core/transaction"
	storetypes "cosmossdk.io/store/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/evmos/ethermint/app/ante"
	"github.com/evmos/ethermint/server/config"
	"github.com/evmos/ethermint/tests"
	ethermint "github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm/statedb"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
)

func (suite *AnteTestSuite) TestNewEthAccountVerificationDecorator() {
	addr := tests.GenerateAddress()

	tx := evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 1, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil)
	tx.From = addr.Bytes()

	var vmdb *statedb.StateDB

	testCases := []struct {
		name     string
		msgs     []sdk.Msg
		malleate func()
		checkTx  bool
		expPass  bool
	}{
		{"not CheckTx", nil, func() {}, false, true},
		{"invalid transaction type", invalidTx{}.GetMsgs(), func() {}, true, false},
		{
			"sender not set to msg",
			evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 1, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil).GetMsgs(),
			func() {},
			true,
			false,
		},
		{
			"sender not EOA",
			tx.GetMsgs(),
			func() {
				// set not as an EOA
				vmdb.SetCode(addr, []byte("1"))
			},
			true,
			false,
		},
		{
			"not enough balance to cover tx cost",
			tx.GetMsgs(),
			func() {
				// reset back to EOA
				vmdb.SetCode(addr, nil)
			},
			true,
			false,
		},
		{
			"success new account",
			tx.GetMsgs(),
			func() {
				vmdb.AddBalance(addr, big.NewInt(1000000))
			},
			true,
			true,
		},
		{
			"success existing account",
			tx.GetMsgs(),
			func() {
				acc := suite.app.AuthKeeper.NewAccountWithAddress(suite.ctx, addr.Bytes())
				suite.app.AuthKeeper.SetAccount(suite.ctx, acc)

				vmdb.AddBalance(addr, big.NewInt(1000000))
			},
			true,
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			vmdb = suite.StateDB()
			tc.malleate()
			suite.Require().NoError(vmdb.Commit())

			accountGetter := ante.NewCachedAccountGetter(suite.ctx, suite.app.AuthKeeper)
			err := ante.VerifyEthAccount(suite.ctx.WithIsCheckTx(tc.checkTx), tc.msgs, suite.app.EvmKeeper, evmtypes.DefaultEVMDenom, accountGetter)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *AnteTestSuite) TestEthNonceVerificationDecorator() {
	suite.SetupTest()

	addr := tests.GenerateAddress()

	tx := evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 1, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil)
	tx.From = addr.Bytes()

	testCases := []struct {
		name      string
		msgs      []sdk.Msg
		malleate  func()
		reCheckTx bool
		expPass   bool
	}{
		{"ReCheckTx", invalidTx{}.GetMsgs(), func() {}, true, false},
		{"invalid transaction type", invalidTx{}.GetMsgs(), func() {}, false, false},
		{"sender account not found", tx.GetMsgs(), func() {}, false, false},
		{
			"sender nonce missmatch",
			tx.GetMsgs(),
			func() {
				acc := suite.app.AuthKeeper.NewAccountWithAddress(suite.ctx, addr.Bytes())
				suite.app.AuthKeeper.SetAccount(suite.ctx, acc)
			},
			false,
			false,
		},
		{
			"success",
			tx.GetMsgs(),
			func() {
				acc := suite.app.AuthKeeper.NewAccountWithAddress(suite.ctx, addr.Bytes())
				suite.Require().NoError(acc.SetSequence(1))
				suite.app.AuthKeeper.SetAccount(suite.ctx, acc)
			},
			false,
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			tc.malleate()
			accountGetter := ante.NewCachedAccountGetter(suite.ctx, suite.app.AuthKeeper)
			err := ante.CheckAndSetEthSenderNonce(suite.ctx.WithIsReCheckTx(tc.reCheckTx), tc.msgs, suite.app.AuthKeeper, false, accountGetter)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

type multiTx struct {
	Msgs []sdk.Msg
}

func (msg *multiTx) GetMsgs() []sdk.Msg {
	return msg.Msgs
}

func (msg *multiTx) GetMsgsV2() ([]proto.Message, error) {
	return nil, errors.New("not implemented")
}

func (msg *multiTx) Hash() [32]byte { return [32]byte{} }

func (msg *multiTx) GetSenders() ([]transaction.Identity, error) { return nil, nil }

func (msg *multiTx) GetReflectMessages() ([]protoreflect.Message, error) { return nil, nil }

func (msg *multiTx) GetMessages() ([]sdk.Msg, error) { return []sdk.Msg{nil}, nil }

func (msg *multiTx) GetGasLimit() (uint64, error) { return 0, nil }

func (msg *multiTx) Bytes() []byte { return nil }

func (suite *AnteTestSuite) TestEthGasConsumeDecorator() {
	evmParams := suite.app.EvmKeeper.GetParams(suite.ctx)
	chainID := suite.app.EvmKeeper.ChainID()
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	baseFee := suite.app.EvmKeeper.GetBaseFee(suite.ctx, ethCfg)
	rules := ethCfg.Rules(big.NewInt(suite.ctx.BlockHeight()), ethCfg.MergeNetsplitBlock != nil, uint64(suite.ctx.BlockHeader().Time.Unix()))

	addr := tests.GenerateAddress()

	txGasLimit := uint64(1000)
	tx := evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 1, big.NewInt(10), txGasLimit, big.NewInt(1), nil, nil, nil, nil)
	tx.From = addr.Bytes()

	suite.Require().Equal(int64(765625000), baseFee.Int64())

	gasPrice := new(big.Int).Add(baseFee, evmtypes.DefaultPriorityReduction.BigInt())

	tx2GasLimit := uint64(1000000)
	tx2 := evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 1, big.NewInt(10), tx2GasLimit, gasPrice, nil, nil, nil, &ethtypes.AccessList{{Address: addr, StorageKeys: nil}})
	tx2.From = addr.Bytes()
	tx2Priority := int64(1)

	tx3GasLimit := ethermint.BlockGasLimit(suite.ctx) + uint64(1)
	tx3 := evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 1, big.NewInt(10), tx3GasLimit, gasPrice, nil, nil, nil, &ethtypes.AccessList{{Address: addr, StorageKeys: nil}})

	dynamicFeeTx := evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 1, big.NewInt(10), tx2GasLimit,
		nil, // gasPrice
		new(big.Int).Add(baseFee, big.NewInt(evmtypes.DefaultPriorityReduction.Int64()*2)), // gasFeeCap
		evmtypes.DefaultPriorityReduction.BigInt(),                                         // gasTipCap
		nil, &ethtypes.AccessList{{Address: addr, StorageKeys: nil}})
	dynamicFeeTx.From = addr.Bytes()
	dynamicFeeTxPriority := int64(1)

	maxGasLimitTx := evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 1, big.NewInt(10), math.MaxUint64, gasPrice, nil, nil, nil, &ethtypes.AccessList{{Address: addr, StorageKeys: nil}})
	maxGasLimitTx.From = addr.Bytes()

	var vmdb *statedb.StateDB

	testCases := []struct {
		name        string
		msgs        []sdk.Msg
		gasLimit    uint64
		malleate    func()
		expPass     bool
		expPanic    bool
		expPriority int64
		err         error
	}{
		{"invalid transaction type", invalidTx{}.GetMsgs(), math.MaxUint64, func() {}, false, false, 0, nil},
		{
			"sender not found",
			evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 1, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil).GetMsgs(),
			math.MaxUint64,
			func() {},
			false, false,
			0,
			nil,
		},
		{
			"gas limit too low",
			tx.GetMsgs(),
			math.MaxUint64,
			func() {},
			false, false,
			0,
			nil,
		},
		{
			"gas limit above block gas limit",
			tx3.GetMsgs(),
			math.MaxUint64,
			func() {},
			false, false,
			0,
			nil,
		},
		{
			"not enough balance for fees",
			tx2.GetMsgs(),
			math.MaxUint64,
			func() {},
			false, false,
			0,
			nil,
		},
		{
			"not enough tx gas",
			tx2.GetMsgs(),
			0,
			func() {
				vmdb.AddBalance(addr, big.NewInt(1000000))
			},
			false, true,
			0,
			nil,
		},
		{
			"not enough block gas",
			tx2.GetMsgs(),
			0,
			func() {
				vmdb.AddBalance(addr, big.NewInt(1000000))
				suite.ctx = suite.ctx.WithBlockGasMeter(storetypes.NewGasMeter(1))
			},
			false, true,
			0,
			nil,
		},
		{
			"gas limit overflow",
			[]sdk.Msg{maxGasLimitTx, tx2},
			math.MaxUint64,
			func() {
				limit := new(big.Int).SetUint64(math.MaxUint64)
				balance := new(big.Int).Mul(limit, gasPrice)
				vmdb.AddBalance(addr, balance)
			},
			false, false,
			0,
			fmt.Errorf("gasWanted(%d) + gasLimit(%d) overflow", maxGasLimitTx.GetGas(), tx2.GetGas()),
		},
		{
			"success - legacy tx",
			tx2.GetMsgs(),
			tx2GasLimit, // it's capped
			func() {
				vmdb.AddBalance(addr, big.NewInt(1001000000000000))
				suite.ctx = suite.ctx.WithBlockGasMeter(storetypes.NewGasMeter(10000000000000000000))
			},
			true, false,
			tx2Priority,
			nil,
		},
		{
			"success - dynamic fee tx",
			dynamicFeeTx.GetMsgs(),
			tx2GasLimit, // it's capped
			func() {
				vmdb.AddBalance(addr, big.NewInt(1001000000000000))
				suite.ctx = suite.ctx.WithBlockGasMeter(storetypes.NewGasMeter(10000000000000000000))
			},
			true, false,
			dynamicFeeTxPriority,
			nil,
		},
		{
			"success - gas limit on gasMeter is set on ReCheckTx mode",
			dynamicFeeTx.GetMsgs(),
			tx2GasLimit, // it's capped
			func() {
				vmdb.AddBalance(addr, big.NewInt(1001000000000000))
				suite.ctx = suite.ctx.WithIsReCheckTx(true)
			},
			true, false,
			1,
			nil,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			vmdb = suite.StateDB()
			tc.malleate()
			suite.Require().NoError(vmdb.Commit())

			if tc.expPanic {
				suite.Require().Panics(func() {
					_, _ = ante.CheckEthGasConsume(
						suite.ctx.WithIsCheckTx(true).WithGasMeter(storetypes.NewGasMeter(1)), tc.msgs,
						rules, suite.app.EvmKeeper, baseFee, config.DefaultMaxTxGasWanted, evmtypes.DefaultEVMDenom,
					)
				})
				return
			}

			ctx, err := ante.CheckEthGasConsume(
				suite.ctx.WithIsCheckTx(true).WithGasMeter(storetypes.NewInfiniteGasMeter()), tc.msgs,
				rules, suite.app.EvmKeeper, baseFee, config.DefaultMaxTxGasWanted, evmtypes.DefaultEVMDenom,
			)
			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(tc.expPriority, ctx.Priority())
			} else {
				if tc.err != nil {
					suite.Require().ErrorContains(err, tc.err.Error())
				} else {
					suite.Require().Error(err)
				}
			}
			suite.Require().Equal(tc.gasLimit, ctx.GasMeter().Limit())
		})
	}
}

func (suite *AnteTestSuite) TestCanTransferDecorator() {
	addr, privKey := tests.NewAddrKey()
	suite.app.FeeMarketKeeper.SetBaseFee(suite.ctx, big.NewInt(100))

	evmParams := suite.app.EvmKeeper.GetParams(suite.ctx)
	chainID := suite.app.EvmKeeper.ChainID()
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	baseFee := suite.app.EvmKeeper.GetBaseFee(suite.ctx, ethCfg)
	rules := ethCfg.Rules(big.NewInt(suite.ctx.BlockHeight()), ethCfg.MergeNetsplitBlock != nil, uint64(suite.ctx.BlockHeader().Time.Unix()))

	tx := evmtypes.NewTxContract(
		suite.app.EvmKeeper.ChainID(),
		1,
		big.NewInt(10),
		1000,
		big.NewInt(150),
		big.NewInt(200),
		nil,
		nil,
		&ethtypes.AccessList{},
	)
	tx2 := evmtypes.NewTxContract(
		suite.app.EvmKeeper.ChainID(),
		1,
		big.NewInt(10),
		1000,
		big.NewInt(150),
		big.NewInt(200),
		nil,
		nil,
		&ethtypes.AccessList{},
	)
	tx3 := evmtypes.NewTxContract(
		suite.app.EvmKeeper.ChainID(),
		1,
		big.NewInt(-10),
		1000,
		big.NewInt(150),
		big.NewInt(200),
		nil,
		nil,
		&ethtypes.AccessList{},
	)

	for _, tx := range []*evmtypes.MsgEthereumTx{tx, tx3} {
		tx.From = addr.Bytes()

		err := tx.Sign(suite.ethSigner, tests.NewSigner(privKey))
		suite.Require().NoError(err)
	}

	var vmdb *statedb.StateDB

	testCases := []struct {
		name     string
		msgs     []sdk.Msg
		malleate func()
		expPass  bool
	}{
		{"invalid transaction type", invalidTx{}.GetMsgs(), func() {}, false},
		{"AsMessage failed", tx2.GetMsgs(), func() {}, false},
		{"negative value", tx3.GetMsgs(), func() {}, false},
		{
			"evm CanTransfer failed",
			tx.GetMsgs(),
			func() {
				acc := suite.app.AuthKeeper.NewAccountWithAddress(suite.ctx, addr.Bytes())
				suite.app.AuthKeeper.SetAccount(suite.ctx, acc)
			},
			false,
		},
		{
			"success",
			tx.GetMsgs(),
			func() {
				acc := suite.app.AuthKeeper.NewAccountWithAddress(suite.ctx, addr.Bytes())
				suite.app.AuthKeeper.SetAccount(suite.ctx, acc)

				vmdb.AddBalance(addr, big.NewInt(1000000))
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			vmdb = suite.StateDB()
			tc.malleate()
			suite.Require().NoError(vmdb.Commit())

			err := ante.CheckEthCanTransfer(
				suite.ctx.WithIsCheckTx(true), tc.msgs,
				baseFee, rules, suite.app.EvmKeeper, &evmParams,
			)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *AnteTestSuite) TestEthIncrementSenderSequenceDecorator() {
	addr, privKey := tests.NewAddrKey()

	contract := evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 0, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil)
	contract.From = addr.Bytes()
	err := contract.Sign(suite.ethSigner, tests.NewSigner(privKey))
	suite.Require().NoError(err)

	to := tests.GenerateAddress()
	tx := evmtypes.NewTx(suite.app.EvmKeeper.ChainID(), 0, &to, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil)
	tx.From = addr.Bytes()
	err = tx.Sign(suite.ethSigner, tests.NewSigner(privKey))
	suite.Require().NoError(err)

	tx2 := evmtypes.NewTx(suite.app.EvmKeeper.ChainID(), 1, &to, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil)
	tx2.From = addr.Bytes()
	err = tx2.Sign(suite.ethSigner, tests.NewSigner(privKey))
	suite.Require().NoError(err)

	testCases := []struct {
		name     string
		msgs     []sdk.Msg
		malleate func()
		expPass  bool
		expPanic bool
	}{
		{
			"invalid transaction type",
			invalidTx{}.GetMsgs(),
			func() {},
			false, false,
		},
		{
			"no signers",
			evmtypes.NewTx(suite.app.EvmKeeper.ChainID(), 1, &to, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil).GetMsgs(),
			func() {},
			false, false,
		},
		{
			"account not set to store",
			tx.GetMsgs(),
			func() {},
			true, false,
		},
		{
			"success - create contract",
			contract.GetMsgs(),
			func() {
				acc := suite.app.AuthKeeper.NewAccountWithAddress(suite.ctx, addr.Bytes())
				suite.app.AuthKeeper.SetAccount(suite.ctx, acc)
			},
			true, false,
		},
		{
			"success - call",
			tx2.GetMsgs(),
			func() {},
			true, false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			tc.malleate()
			accountGetter := ante.NewCachedAccountGetter(suite.ctx, suite.app.AuthKeeper)

			if tc.expPanic {
				suite.Require().Panics(func() {
					_ = ante.CheckAndSetEthSenderNonce(suite.ctx, tc.msgs, suite.app.AuthKeeper, false, accountGetter)
				})
				return
			}

			err := ante.CheckAndSetEthSenderNonce(suite.ctx, tc.msgs, suite.app.AuthKeeper, false, accountGetter)

			if tc.expPass {
				suite.Require().NoError(err)
				msg := tc.msgs[0].(*evmtypes.MsgEthereumTx)

				txData := msg.AsTransaction()
				suite.Require().NotNil(txData)

				nonce := suite.app.EvmKeeper.GetNonce(suite.ctx, addr)
				suite.Require().Equal(txData.Nonce()+1, nonce)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
