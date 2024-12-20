package keeper_test

import (
	"context"
	"math/big"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/suite"

	banktypes "cosmossdk.io/x/bank/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/server"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/evmos/ethermint/tests"
	"github.com/evmos/ethermint/testutil"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	feemarkettypes "github.com/evmos/ethermint/x/feemarket/types"
)

var s *IntegrationTestSuite

func TestFeemarket(t *testing.T) {
	// Run Ginkgo integration tests
	s = new(IntegrationTestSuite)
	suite.Run(t, s)

	RegisterFailHandler(Fail)
	RunSpecs(t, "IntegrationTestSuite")
}

type txParams struct {
	gasPrice  *big.Int
	gasFeeCap *big.Int
	gasTipCap *big.Int
	accesses  *ethtypes.AccessList
}

var _ = Describe("Feemarket", func() {
	var (
		msg banktypes.MsgSend
	)

	Describe("Performing Cosmos transactions", func() {
		Context("with min-gas-prices (local) < MinGasPrices (feemarket param)", func() {
			BeforeEach(func() {
				msg = setupTest("1", sdkmath.LegacyNewDec(3), sdkmath.ZeroInt())
			})

			Context("during CheckTx", func() {
				It("should reject transactions with gasPrice < MinGasPrices", func() {
					gasPrice := sdkmath.NewInt(2)
					res := checkTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
					Expect(
						strings.Contains(res.GetLog(),
							"provided fee < minimum global fee"),
					).To(BeTrue(), res.GetLog())
				})

				It("should accept transactions with gasPrice >= MinGasPrices", func() {
					gasPrice := sdkmath.NewInt(3)
					res := checkTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
				})
			})

			Context("during DeliverTx", func() {
				It("should reject transactions with gasPrice < MinGasPrices", func() {
					gasPrice := sdkmath.NewInt(2)
					res := deliverTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
					Expect(
						strings.Contains(res.GetLog(),
							"provided fee < minimum global fee"),
					).To(BeTrue(), res.GetLog())
				})

				It("should accept transactions with gasPrice >= MinGasPrices", func() {
					gasPrice := sdkmath.NewInt(3)
					res := deliverTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
				})
			})
		})

		Context("with min-gas-prices (local) == MinGasPrices (feemarket param)", func() {
			BeforeEach(func() {
				msg = setupTest("3", sdkmath.LegacyNewDec(3), sdkmath.ZeroInt())
			})

			Context("during CheckTx", func() {
				It("should reject transactions with gasPrice < min-gas-prices", func() {
					gasPrice := sdkmath.NewInt(2)
					res := checkTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
					Expect(
						strings.Contains(res.GetLog(),
							"insufficient fee"),
					).To(BeTrue(), res.GetLog())
				})

				It("should accept transactions with gasPrice >= MinGasPrices", func() {
					gasPrice := sdkmath.NewInt(3)
					res := checkTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
				})
			})

			Context("during DeliverTx", func() {
				It("should reject transactions with gasPrice < MinGasPrices", func() {
					gasPrice := sdkmath.NewInt(2)
					res := deliverTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
					Expect(
						strings.Contains(res.GetLog(),
							"provided fee < minimum global fee"),
					).To(BeTrue(), res.GetLog())
				})

				It("should accept transactions with gasPrice >= MinGasPrices", func() {
					gasPrice := sdkmath.NewInt(3)
					res := deliverTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
				})
			})
		})

		Context("with MinGasPrices (feemarket param) < min-gas-prices (local)", func() {
			BeforeEach(func() {
				msg = setupTest("5", sdkmath.LegacyNewDec(3), sdkmath.NewInt(5))
			})
			Context("during CheckTx", func() {
				It("should reject transactions with gasPrice < MinGasPrices", func() {
					gasPrice := sdkmath.NewInt(2)
					res := checkTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
					Expect(
						strings.Contains(res.GetLog(),
							"insufficient fee"),
					).To(BeTrue(), res.GetLog())
				})

				It("should reject transactions with MinGasPrices < gasPrice < baseFee", func() {
					gasPrice := sdkmath.NewInt(4)
					res := checkTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
					Expect(
						strings.Contains(res.GetLog(),
							"insufficient fee"),
					).To(BeTrue(), res.GetLog())
				})

				It("should accept transactions with gasPrice >= baseFee", func() {
					gasPrice := sdkmath.NewInt(5)
					res := checkTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
				})
			})

			Context("during DeliverTx", func() {
				It("should reject transactions with gasPrice < MinGasPrices", func() {
					gasPrice := sdkmath.NewInt(2)
					res := deliverTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
					Expect(
						strings.Contains(res.GetLog(),
							"provided fee < minimum global fee"),
					).To(BeTrue(), res.GetLog())
				})

				It("should reject transactions with MinGasPrices < gasPrice < baseFee", func() {
					gasPrice := sdkmath.NewInt(4)
					res := checkTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
					Expect(
						strings.Contains(res.GetLog(),
							"insufficient fee"),
					).To(BeTrue(), res.GetLog())
				})
				It("should accept transactions with gasPrice >= baseFee", func() {
					gasPrice := sdkmath.NewInt(5)
					res := deliverTx(&gasPrice, &msg)
					Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
				})
			})
		})
	})

	Describe("Performing EVM transactions", func() {
		type getprices func() txParams

		Context("with BaseFee (feemarket) < MinGasPrices (feemarket param)", func() {
			var (
				baseFee      int64
				minGasPrices int64
			)

			BeforeEach(func() {
				baseFee = 10_000_000_000
				minGasPrices = baseFee + 30_000_000_000

				// Note that the tests run the same transactions with `gasLimit =
				// 100000`. With the fee calculation `Fee = (baseFee + tip) * gasLimit`,
				// a `minGasPrices = 40_000_000_000` results in `minGlobalFee =
				// 4000000000000000`
				setupTest("1", sdkmath.LegacyNewDec(minGasPrices), sdkmath.NewInt(baseFee))
			})

			Context("during CheckTx", func() {
				DescribeTable("should reject transactions with EffectivePrice < MinGasPrices",
					func(malleate getprices) {
						p := malleate()
						res := s.CheckTx(prepareEthTx(p))
						Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
						Expect(
							strings.Contains(res.GetLog(),
								"provided fee < minimum global fee"),
						).To(BeTrue(), res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{big.NewInt(minGasPrices - 10_000_000_000), nil, nil, nil}
					}),
					Entry("dynamic tx with GasFeeCap < MinGasPrices, no gasTipCap", func() txParams {
						return txParams{nil, big.NewInt(minGasPrices - 10_000_000_000), big.NewInt(0), &ethtypes.AccessList{}}
					}),
					Entry("dynamic tx with GasFeeCap < MinGasPrices, max gasTipCap", func() txParams {
						// Note that max priority fee per gas can't be higher than the max fee per gas (gasFeeCap), i.e. 30_000_000_000)
						return txParams{nil, big.NewInt(minGasPrices - 10_000_000_000), big.NewInt(30_000_000_000), &ethtypes.AccessList{}}
					}),
				)

				DescribeTable("should accept transactions with gasPrice >= MinGasPrices",
					func(malleate getprices) {
						p := malleate()
						res := s.CheckTx(prepareEthTx(p))
						Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{big.NewInt(minGasPrices), nil, nil, nil}
					}),
					// Note that this tx is not rejected on CheckTx, but not on DeliverTx,
					// as the baseFee is set to minGasPrices during DeliverTx when baseFee
					// < minGasPrices
					Entry("dynamic tx with GasFeeCap > MinGasPrices, EffectivePrice > MinGasPrices", func() txParams {
						return txParams{nil, big.NewInt(minGasPrices), big.NewInt(30_000_000_000), &ethtypes.AccessList{}}
					}),
				)
			})

			Context("during DeliverTx", func() {
				DescribeTable("should reject transactions with gasPrice < MinGasPrices",
					func(malleate getprices) {
						p := malleate()
						res := s.DeliverTx(prepareEthTx(p))
						Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
						Expect(
							strings.Contains(res.GetLog(),
								"provided fee < minimum global fee"),
						).To(BeTrue(), res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{big.NewInt(minGasPrices - 10_000_000_000), nil, nil, nil}
					}),
					Entry("dynamic tx with GasFeeCap < MinGasPrices, no gasTipCap", func() txParams {
						return txParams{nil, big.NewInt(minGasPrices - 10_000_000_000), big.NewInt(0), &ethtypes.AccessList{}}
					}),
					Entry("dynamic tx with GasFeeCap < MinGasPrices, max gasTipCap", func() txParams {
						// Note that max priority fee per gas can't be higher than the max fee per gas (gasFeeCap), i.e. 30_000_000_000)
						return txParams{nil, big.NewInt(minGasPrices - 10_000_000_000), big.NewInt(30_000_000_000), &ethtypes.AccessList{}}
					}),
				)

				DescribeTable("should accept transactions with gasPrice >= MinGasPrices",
					func(malleate getprices) {
						p := malleate()
						res := s.DeliverTx(prepareEthTx(p))
						Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{big.NewInt(minGasPrices + 1), nil, nil, nil}
					}),
					Entry("dynamic tx, EffectivePrice > MinGasPrices", func() txParams {
						return txParams{nil, big.NewInt(minGasPrices + 10_000_000_000), big.NewInt(30_000_000_000), &ethtypes.AccessList{}}
					}),
				)
			})
		})

		Context("with MinGasPrices (feemarket param) < BaseFee (feemarket)", func() {
			var (
				baseFee      int64
				minGasPrices int64
			)

			BeforeEach(func() {
				baseFee = 10_000_000_000
				minGasPrices = baseFee - 5_000_000_000

				// Note that the tests run the same transactions with `gasLimit =
				// 100_000`. With the fee calculation `Fee = (baseFee + tip) * gasLimit`,
				// a `minGasPrices = 5_000_000_000` results in `minGlobalFee =
				// 500_000_000_000_000`
				setupTest("1", sdkmath.LegacyNewDec(minGasPrices), sdkmath.NewInt(baseFee))
			})

			Context("during CheckTx", func() {
				DescribeTable("should reject transactions with gasPrice < MinGasPrices",
					func(malleate getprices) {
						p := malleate()
						res := s.CheckTx(prepareEthTx(p))
						Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
						Expect(
							strings.Contains(res.GetLog(),
								"provided fee < minimum global fee"),
						).To(BeTrue(), res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{big.NewInt(minGasPrices - 1_000_000_000), nil, nil, nil}
					}),
					Entry("dynamic tx with GasFeeCap < MinGasPrices, no gasTipCap", func() txParams {
						return txParams{nil, big.NewInt(minGasPrices - 1_000_000_000), big.NewInt(0), &ethtypes.AccessList{}}
					}),
					Entry("dynamic tx with GasFeeCap < MinGasPrices, max gasTipCap", func() txParams {
						return txParams{nil, big.NewInt(minGasPrices - 1_000_000_000), big.NewInt(minGasPrices - 1_000_000_000), &ethtypes.AccessList{}}
					}),
				)

				DescribeTable("should reject transactions with MinGasPrices < tx gasPrice < EffectivePrice",
					func(malleate getprices) {
						p := malleate()
						res := s.CheckTx(prepareEthTx(p))
						Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
						Expect(
							strings.Contains(res.GetLog(),
								"insufficient fee"),
						).To(BeTrue(), res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{big.NewInt(baseFee - 2_000_000_000), nil, nil, nil}
					}),
					Entry("dynamic tx", func() txParams {
						return txParams{nil, big.NewInt(baseFee - 2_000_000_000), big.NewInt(0), &ethtypes.AccessList{}}
					}),
				)

				DescribeTable("should accept transactions with gasPrice >= EffectivePrice",
					func(malleate getprices) {
						p := malleate()
						res := s.CheckTx(prepareEthTx(p))
						Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{big.NewInt(baseFee), nil, nil, nil}
					}),
					Entry("dynamic tx", func() txParams {
						return txParams{nil, big.NewInt(baseFee), big.NewInt(0), &ethtypes.AccessList{}}
					}),
				)
			})

			Context("during DeliverTx", func() {
				DescribeTable("should reject transactions with gasPrice < MinGasPrices",
					func(malleate getprices) {
						p := malleate()
						res := s.DeliverTx(prepareEthTx(p))
						Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
						Expect(
							strings.Contains(res.GetLog(),
								"provided fee < minimum global fee"),
						).To(BeTrue(), res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{big.NewInt(minGasPrices - 1_000_000_000), nil, nil, nil}
					}),
					Entry("dynamic tx", func() txParams {
						return txParams{nil, big.NewInt(minGasPrices - 1_000_000_000), nil, &ethtypes.AccessList{}}
					}),
				)

				DescribeTable("should reject transactions with MinGasPrices < gasPrice < EffectivePrice",
					func(malleate getprices) {
						p := malleate()
						res := s.DeliverTx(prepareEthTx(p))
						Expect(res.IsOK()).To(Equal(false), "transaction should have failed")
						Expect(
							strings.Contains(res.GetLog(),
								"insufficient fee"),
						).To(BeTrue(), res.GetLog())
					},
					// Note that the baseFee is not 10_000_000_000 anymore but updates to 8_750_000_000 because of the s.Commit
					Entry("legacy tx", func() txParams {
						return txParams{big.NewInt(baseFee - 2_500_000_000), nil, nil, nil}
					}),
					Entry("dynamic tx", func() txParams {
						return txParams{nil, big.NewInt(baseFee - 2_500_000_000), big.NewInt(0), &ethtypes.AccessList{}}
					}),
				)

				DescribeTable("should accept transactions with gasPrice >= EffectivePrice",
					func(malleate getprices) {
						p := malleate()
						res := s.DeliverTx(prepareEthTx(p))
						Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{big.NewInt(baseFee), nil, nil, nil}
					}),
					Entry("dynamic tx", func() txParams {
						return txParams{nil, big.NewInt(baseFee), big.NewInt(0), &ethtypes.AccessList{}}
					}),
				)
			})
		})
	})
})

type IntegrationTestSuite struct {
	testutil.BaseTestSuiteWithAccount
}

// SetupTest sets up a test chain with an example Cosmos send msg,
// given a local (validator config) and a gloabl (feemarket param) minGasPrice
func setupTest(valMinGasPrice string, minGasPrice sdkmath.LegacyDec, baseFee sdkmath.Int) banktypes.MsgSend {
	t := s.T()
	s.SetupTestWithCbAndOpts(
		t,
		nil,
		simtestutil.AppOptionsMap{server.FlagMinGasPrices: valMinGasPrice + evmtypes.DefaultEVMDenom},
	)
	amount, ok := sdkmath.NewIntFromString("10000000000000000000")
	s.Require().True(ok)
	initBalance := sdk.Coins{sdk.Coin{
		Denom:  evmtypes.DefaultEVMDenom,
		Amount: amount,
	}}
	address := sdk.AccAddress(s.Address.Bytes())
	testutil.FundAccount(s.App.BankKeeper, s.Ctx, address, initBalance)
	msg := banktypes.MsgSend{
		FromAddress: address.String(),
		ToAddress:   address.String(),
		Amount: sdk.Coins{sdk.Coin{
			Denom:  evmtypes.DefaultEVMDenom,
			Amount: sdkmath.NewInt(10000),
		}},
	}
	s.Commit(t)
	params := feemarkettypes.DefaultParams()
	params.MinGasPrice = minGasPrice
	s.App.FeeMarketKeeper.SetParams(s.Ctx, params)
	s.App.FeeMarketKeeper.SetBaseFee(s.Ctx, baseFee.BigInt())
	s.Commit(t)
	return msg
}

func prepareCosmosTx(gasPrice *sdkmath.Int, msgs ...sdk.Msg) []byte {
	encodingConfig := s.App.EncodingConfig()
	accountAddress := sdk.AccAddress(s.PrivKey.PubKey().Address().Bytes())

	txBuilder := encodingConfig.TxConfig.NewTxBuilder()

	txBuilder.SetGasLimit(1000000)
	if gasPrice == nil {
		_gasPrice := sdkmath.NewInt(1)
		gasPrice = &_gasPrice
	}
	fees := &sdk.Coins{{Denom: evmtypes.DefaultEVMDenom, Amount: gasPrice.MulRaw(1000000)}}
	txBuilder.SetFeeAmount(*fees)
	err := txBuilder.SetMsgs(msgs...)
	s.Require().NoError(err)

	seq, err := s.App.AccountKeeper.GetSequence(s.Ctx, accountAddress)
	s.Require().NoError(err)

	defaultMode, err := authsigning.APISignModeToInternal(encodingConfig.TxConfig.SignModeHandler().DefaultMode())
	s.Require().NoError(err)

	// First round: we gather all the signer infos. We use the "set empty
	// signature" hack to do that.
	sigV2 := signing.SignatureV2{
		PubKey: s.PrivKey.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  defaultMode,
			Signature: nil,
		},
		Sequence: seq,
	}

	sigsV2 := []signing.SignatureV2{sigV2}

	err = txBuilder.SetSignatures(sigsV2...)
	s.Require().NoError(err)

	// Second round: all signer infos are set, so each signer can sign.
	accNumber := s.App.AccountKeeper.GetAccount(s.Ctx, accountAddress).GetAccountNumber()
	signerData := authsigning.SignerData{
		ChainID:       s.Ctx.ChainID(),
		AccountNumber: accNumber,
		Sequence:      seq,
	}
	sigV2, err = tx.SignWithPrivKey(
		context.Background(),
		defaultMode, signerData,
		txBuilder, s.PrivKey, encodingConfig.TxConfig,
		seq,
	)
	s.Require().NoError(err)

	sigsV2 = []signing.SignatureV2{sigV2}
	err = txBuilder.SetSignatures(sigsV2...)
	s.Require().NoError(err)

	// bz are bytes to be broadcasted over the network
	bz, err := encodingConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
	s.Require().NoError(err)
	return bz
}

func prepareEthTx(p txParams) []byte {
	to := tests.GenerateAddress()
	const gasLimit = uint64(100000)
	msg := s.BuildEthTx(&to, gasLimit, p.gasPrice, p.gasFeeCap, p.gasTipCap, p.accesses, s.PrivKey)
	return s.PrepareEthTx(msg, s.PrivKey)
}

func checkTx(gasPrice *sdkmath.Int, msgs ...sdk.Msg) abci.ResponseCheckTx {
	return s.CheckTx(prepareCosmosTx(gasPrice, msgs...))
}

func deliverTx(gasPrice *sdkmath.Int, msgs ...sdk.Msg) *abci.ExecTxResult {
	return s.DeliverTx(prepareCosmosTx(gasPrice, msgs...))
}
