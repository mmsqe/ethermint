package config

import (
	"cosmossdk.io/x/bank"
	distr "cosmossdk.io/x/distribution"
	"cosmossdk.io/x/gov"
	govclient "cosmossdk.io/x/gov/client"
	paramsclient "cosmossdk.io/x/params/client"
	"cosmossdk.io/x/staking"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/evmos/ethermint/encoding"
	"github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm"
	"github.com/evmos/ethermint/x/feemarket"
)

func MakeConfigForTest(moduleManager module.BasicManager) types.EncodingConfig {
	config := encoding.MakeConfig()
	if moduleManager == nil {
		moduleManager = module.NewBasicManager(
			auth.AppModuleBasic{},
			bank.AppModuleBasic{},
			distr.AppModuleBasic{},
			gov.NewAppModuleBasic([]govclient.ProposalHandler{paramsclient.ProposalHandler}),
			staking.AppModuleBasic{},
			// Ethermint modules
			evm.AppModuleBasic{},
			feemarket.AppModuleBasic{},
		)
	}
	moduleManager.RegisterLegacyAminoCodec(config.Amino)
	moduleManager.RegisterInterfaces(config.InterfaceRegistry)
	return config
}
