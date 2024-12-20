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

func MakeConfigForTest(moduleManager *module.Manager) types.EncodingConfig {
	app := app.NewEthermintApp(
		cmtlog.NewNopLogger(), dbm.NewMemDB(), nil, true,
		simtestutil.NewAppOptionsWithFlagHome(app.DefaultNodeHome),
	)
	encodingConfig := encoding.MakeConfig()
	appCodec := encodingConfig.Codec
	if moduleManager == nil {
		moduleManager = module.NewManager(
			auth.NewAppModule(appCodec, app.AuthKeeper, app.AccountsKeeper, authsims.RandomGenesisAccounts, nil),
			bank.NewAppModule(appCodec, app.BankKeeper, app.AuthKeeper),
			distr.NewAppModule(appCodec, app.DistrKeeper, app.StakingKeeper),
			gov.NewAppModule(appCodec, &app.GovKeeper, app.AuthKeeper, app.BankKeeper, app.PoolKeeper),
			staking.NewAppModule(appCodec, app.StakingKeeper),
			// Ethermint modules
			evm.NewAppModule(appCodec, app.EvmKeeper, app.AuthKeeper, app.AuthKeeper.Accounts, nil),
			feemarket.NewAppModule(appCodec, app.FeeMarketKeeper, nil),
		)
	}
	moduleManager.RegisterLegacyAminoCodec(encodingConfig.Amino)
	moduleManager.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	return encodingConfig
}
