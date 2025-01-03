package config

import (
	cmtlog "cosmossdk.io/log"
	"cosmossdk.io/x/bank"
	distr "cosmossdk.io/x/distribution"
	"cosmossdk.io/x/gov"
	"cosmossdk.io/x/staking"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	"github.com/evmos/ethermint/app"
	"github.com/evmos/ethermint/encoding"
	"github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm"
	"github.com/evmos/ethermint/x/feemarket"
)

func MakeConfigForTest() types.EncodingConfig {
	a := app.NewEthermintApp(
		cmtlog.NewNopLogger(), dbm.NewMemDB(), nil, true,
		simtestutil.NewAppOptionsWithFlagHome(app.DefaultNodeHome),
	)
	encodingConfig := encoding.MakeConfig()
	appCodec := encodingConfig.Codec
	moduleManager := module.NewManager(
		auth.NewAppModule(appCodec, a.AuthKeeper, a.AccountsKeeper, authsims.RandomGenesisAccounts, nil),
		bank.NewAppModule(appCodec, a.BankKeeper, a.AuthKeeper),
		distr.NewAppModule(appCodec, a.DistrKeeper, a.StakingKeeper),
		gov.NewAppModule(appCodec, &a.GovKeeper, a.AuthKeeper, a.BankKeeper, a.PoolKeeper),
		staking.NewAppModule(appCodec, a.StakingKeeper),
		// Ethermint modules
		evm.NewAppModule(appCodec, a.EvmKeeper, a.AuthKeeper, a.AuthKeeper.Accounts, nil),
		feemarket.NewAppModule(appCodec, a.FeeMarketKeeper, nil),
	)
	moduleManager.RegisterLegacyAminoCodec(encodingConfig.Amino)
	moduleManager.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	return encodingConfig
}
