package ante

import (
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/ethereum/go-ethereum/params"
	"github.com/evmos/ethermint/encoding"
	ethermint "github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm/types"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	feemarkettypes "github.com/evmos/ethermint/x/feemarket/types"
)

type MockEVMKeeper struct {
	BaseFee        *big.Int
	EnableLondonHF bool
}

func (m MockEVMKeeper) GetBaseFee(ctx sdk.Context, ethCfg *params.ChainConfig) *big.Int {
	if m.EnableLondonHF {
		return m.BaseFee
	}
	return nil
}

func (m MockEVMKeeper) GetParams(ctx sdk.Context) evmtypes.Params {
	params := evmtypes.DefaultParams()
	if !m.EnableLondonHF {
		params.ChainConfig.LondonBlock = nil
	}
	return params
}

func (m MockEVMKeeper) ChainID() *big.Int {
	return big.NewInt(9000)
}

func TestSDKTxFeeChecker(t *testing.T) {
	// testCases:
	//   fallback
	//      genesis tx
	//      checkTx, validate with min-gas-prices
	//      deliverTx, no validation
	//   dynamic fee
	//      with extension option
	//      without extension option
	//      london hardfork enableness
	encodingConfig := encoding.MakeConfig()
	minGasPrices := sdk.NewDecCoins(sdk.NewDecCoin("aphoton", sdkmath.NewInt(10)))
	logger := log.NewNopLogger()
	genesisCtx := sdk.NewContext(nil, false, logger)
	checkTxCtx := sdk.NewContext(nil, true, logger).WithMinGasPrices(minGasPrices).WithBlockHeight(1)
	deliverTxCtx := sdk.NewContext(nil, false, logger).WithBlockHeight(1)

	testCases := []struct {
		name        string
		ctx         sdk.Context
		keeper      MockEVMKeeper
		buildTx     func() sdk.Tx
		expFees     string
		expPriority int64
		expSuccess  bool
	}{
		{
			"success, genesis tx",
			genesisCtx,
			MockEVMKeeper{},
			func() sdk.Tx {
				return encodingConfig.TxConfig.NewTxBuilder().GetTx()
			},
			"",
			0,
			true,
		},
		{
			"fail, min-gas-prices",
			checkTxCtx,
			MockEVMKeeper{},
			func() sdk.Tx {
				return encodingConfig.TxConfig.NewTxBuilder().GetTx()
			},
			"",
			0,
			false,
		},
		{
			"success, min-gas-prices",
			checkTxCtx,
			MockEVMKeeper{},
			func() sdk.Tx {
				txBuilder := encodingConfig.TxConfig.NewTxBuilder()
				txBuilder.SetGasLimit(1)
				txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("aphoton", sdkmath.NewInt(10))))
				return txBuilder.GetTx()
			},
			"10aphoton",
			0,
			true,
		},
		{
			"success, min-gas-prices deliverTx",
			deliverTxCtx,
			MockEVMKeeper{},
			func() sdk.Tx {
				return encodingConfig.TxConfig.NewTxBuilder().GetTx()
			},
			"",
			0,
			true,
		},
		{
			"fail, dynamic fee",
			deliverTxCtx,
			MockEVMKeeper{
				EnableLondonHF: true, BaseFee: big.NewInt(1),
			},
			func() sdk.Tx {
				txBuilder := encodingConfig.TxConfig.NewTxBuilder()
				txBuilder.SetGasLimit(1)
				return txBuilder.GetTx()
			},
			"",
			0,
			false,
		},
		{
			"success, dynamic fee",
			deliverTxCtx,
			MockEVMKeeper{
				EnableLondonHF: true, BaseFee: big.NewInt(10),
			},
			func() sdk.Tx {
				txBuilder := encodingConfig.TxConfig.NewTxBuilder()
				txBuilder.SetGasLimit(1)
				txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("aphoton", sdkmath.NewInt(10))))
				return txBuilder.GetTx()
			},
			"10aphoton",
			0,
			true,
		},
		{
			"success, dynamic fee priority",
			deliverTxCtx,
			MockEVMKeeper{
				EnableLondonHF: true, BaseFee: big.NewInt(10),
			},
			func() sdk.Tx {
				txBuilder := encodingConfig.TxConfig.NewTxBuilder()
				txBuilder.SetGasLimit(1)
				txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("aphoton", sdkmath.NewInt(10).Mul(types.DefaultPriorityReduction).Add(sdkmath.NewInt(10)))))
				return txBuilder.GetTx()
			},
			"10000010aphoton",
			10,
			true,
		},
		{
			"success, dynamic fee empty tipFeeCap",
			deliverTxCtx,
			MockEVMKeeper{
				EnableLondonHF: true, BaseFee: big.NewInt(10),
			},
			func() sdk.Tx {
				txBuilder := encodingConfig.TxConfig.NewTxBuilder().(authtx.ExtensionOptionsTxBuilder)
				txBuilder.SetGasLimit(1)
				txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("aphoton", sdkmath.NewInt(10).Mul(types.DefaultPriorityReduction))))

				option, err := codectypes.NewAnyWithValue(&ethermint.ExtensionOptionDynamicFeeTx{})
				require.NoError(t, err)
				txBuilder.SetExtensionOptions(option)
				return txBuilder.GetTx()
			},
			"10aphoton",
			0,
			true,
		},
		{
			"success, dynamic fee tipFeeCap",
			deliverTxCtx,
			MockEVMKeeper{
				EnableLondonHF: true, BaseFee: big.NewInt(10),
			},
			func() sdk.Tx {
				txBuilder := encodingConfig.TxConfig.NewTxBuilder().(authtx.ExtensionOptionsTxBuilder)
				txBuilder.SetGasLimit(1)
				txBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewCoin("aphoton", sdkmath.NewInt(10).Mul(types.DefaultPriorityReduction).Add(sdkmath.NewInt(10)))))

				option, err := codectypes.NewAnyWithValue(&ethermint.ExtensionOptionDynamicFeeTx{
					MaxPriorityPrice: sdkmath.NewInt(5).Mul(types.DefaultPriorityReduction),
				})
				require.NoError(t, err)
				txBuilder.SetExtensionOptions(option)
				return txBuilder.GetTx()
			},
			"5000010aphoton",
			5,
			true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			evmParams := tc.keeper.GetParams(tc.ctx)
			feemarketParams := feemarkettypes.Params{
				NoBaseFee: false,
				BaseFee:   sdkmath.NewIntFromBigInt(tc.keeper.BaseFee),
			}
			chainID := tc.keeper.ChainID()
			chainCfg := evmParams.GetChainConfig()
			ethCfg := chainCfg.EthereumConfig(chainID)
			fees, priority, err := NewDynamicFeeChecker(ethCfg, &evmParams, &feemarketParams)(tc.ctx, tc.buildTx())
			if tc.expSuccess {
				require.Equal(t, tc.expFees, fees.String())
				require.Equal(t, tc.expPriority, priority)
			} else {
				require.Error(t, err)
			}
		})
	}
}
