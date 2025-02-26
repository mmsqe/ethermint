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
package app

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"

	authmodulev1 "cosmossdk.io/api/cosmos/auth/module/v1"
	autocliv1 "cosmossdk.io/api/cosmos/autocli/v1"
	reflectionv1 "cosmossdk.io/api/cosmos/reflection/v1"
	"cosmossdk.io/client/v2/autocli"
	"cosmossdk.io/x/accounts"
	"cosmossdk.io/x/accounts/accountstd"
	baseaccount "cosmossdk.io/x/accounts/defaults/base"
	"cosmossdk.io/x/accounts/defaults/lockup"
	"cosmossdk.io/x/accounts/defaults/multisig"
	txdecode "cosmossdk.io/x/tx/decode"
	"cosmossdk.io/x/tx/signing"
	runtimeservices "github.com/cosmos/cosmos-sdk/runtime/services"
	"github.com/cosmos/cosmos-sdk/server"
	sigtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/gogoproto/proto"

	"github.com/gorilla/mux"
	"github.com/spf13/cast"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtcrypto "github.com/cometbft/cometbft/crypto"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/client/grpc/node"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server/api"
	"github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"

	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	storetypes "cosmossdk.io/store/types"
	"cosmossdk.io/x/authz"
	authzkeeper "cosmossdk.io/x/authz/keeper"
	authzmodule "cosmossdk.io/x/authz/module"
	"cosmossdk.io/x/bank"
	bankkeeper "cosmossdk.io/x/bank/keeper"
	banktypes "cosmossdk.io/x/bank/types"
	"cosmossdk.io/x/consensus"
	distr "cosmossdk.io/x/distribution"
	distrkeeper "cosmossdk.io/x/distribution/keeper"
	distrtypes "cosmossdk.io/x/distribution/types"
	"cosmossdk.io/x/evidence"
	evidencekeeper "cosmossdk.io/x/evidence/keeper"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	feegrantkeeper "cosmossdk.io/x/feegrant/keeper"
	feegrantmodule "cosmossdk.io/x/feegrant/module"
	"cosmossdk.io/x/gov"
	govkeeper "cosmossdk.io/x/gov/keeper"
	govtypes "cosmossdk.io/x/gov/types"
	govv1beta1 "cosmossdk.io/x/gov/types/v1beta1"
	"cosmossdk.io/x/mint"
	mintkeeper "cosmossdk.io/x/mint/keeper"
	minttypes "cosmossdk.io/x/mint/types"
	"cosmossdk.io/x/params"
	paramskeeper "cosmossdk.io/x/params/keeper"
	paramstypes "cosmossdk.io/x/params/types"
	paramproposal "cosmossdk.io/x/params/types/proposal"
	"cosmossdk.io/x/protocolpool"
	poolkeeper "cosmossdk.io/x/protocolpool/keeper"
	pooltypes "cosmossdk.io/x/protocolpool/types"
	"cosmossdk.io/x/slashing"
	slashingkeeper "cosmossdk.io/x/slashing/keeper"
	slashingtypes "cosmossdk.io/x/slashing/types"
	"cosmossdk.io/x/staking"
	stakingkeeper "cosmossdk.io/x/staking/keeper"
	stakingtypes "cosmossdk.io/x/staking/types"
	"cosmossdk.io/x/upgrade"
	upgradekeeper "cosmossdk.io/x/upgrade/keeper"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	mempool "github.com/cosmos/cosmos-sdk/types/mempool"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/auth/ante/unorderedtx"
	authcodec "github.com/cosmos/cosmos-sdk/x/auth/codec"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/cosmos/cosmos-sdk/x/auth/posthandler"
	authsims "github.com/cosmos/cosmos-sdk/x/auth/simulation"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	txmodule "github.com/cosmos/cosmos-sdk/x/auth/tx/config"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/auth/vesting"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"

	"github.com/cosmos/ibc-go/v9/modules/apps/transfer"
	ibctransferkeeper "github.com/cosmos/ibc-go/v9/modules/apps/transfer/keeper"
	ibctransfertypes "github.com/cosmos/ibc-go/v9/modules/apps/transfer/types"
	ibc "github.com/cosmos/ibc-go/v9/modules/core"
	ibcclienttypes "github.com/cosmos/ibc-go/v9/modules/core/02-client/types"
	ibcconnectiontypes "github.com/cosmos/ibc-go/v9/modules/core/03-connection/types"
	porttypes "github.com/cosmos/ibc-go/v9/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v9/modules/core/exported"
	ibckeeper "github.com/cosmos/ibc-go/v9/modules/core/keeper"
	ibctm "github.com/cosmos/ibc-go/v9/modules/light-clients/07-tendermint"

	"github.com/evmos/ethermint/app/ante"
	"github.com/evmos/ethermint/client/docs"
	"github.com/evmos/ethermint/encoding"
	"github.com/evmos/ethermint/ethereum/eip712"
	srvconfig "github.com/evmos/ethermint/server/config"
	srvflags "github.com/evmos/ethermint/server/flags"
	ethermint "github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm"
	evmkeeper "github.com/evmos/ethermint/x/evm/keeper"
	v0evmtypes "github.com/evmos/ethermint/x/evm/migrations/v0/types"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	"github.com/evmos/ethermint/x/feemarket"
	feemarketkeeper "github.com/evmos/ethermint/x/feemarket/keeper"
	feemarkettypes "github.com/evmos/ethermint/x/feemarket/types"

	consensusparamkeeper "cosmossdk.io/x/consensus/keeper"
	consensusparamtypes "cosmossdk.io/x/consensus/types"
	"github.com/ethereum/go-ethereum/common"

	// Force-load the tracer engines to trigger registration due to Go-Ethereum v1.10.15 changes
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"
	_ "github.com/ethereum/go-ethereum/eth/tracers/native"
)

func init() {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	DefaultNodeHome = filepath.Join(userHomeDir, ".ethermintd")
}

const appName = "ethermintd"

var (
	// DefaultNodeHome default home directories for the application daemon
	DefaultNodeHome string

	// module account permissions
	moduleAccPerms = []*authmodulev1.ModuleAccountPermission{
		{Account: authtypes.FeeCollectorName},
		{Account: distrtypes.ModuleName},
		{Account: pooltypes.ModuleName},
		{Account: pooltypes.StreamAccount},
		{Account: pooltypes.ProtocolPoolDistrAccount},
		{Account: minttypes.ModuleName, Permissions: []string{authtypes.Minter}},
		{Account: stakingtypes.BondedPoolName, Permissions: []string{authtypes.Burner, stakingtypes.ModuleName}},
		{Account: stakingtypes.NotBondedPoolName, Permissions: []string{authtypes.Burner, stakingtypes.ModuleName}},
		{Account: govtypes.ModuleName, Permissions: []string{authtypes.Burner}},
		{Account: ibctransfertypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
		// used for secure addition and subtraction of balance using module account
		{Account: evmtypes.ModuleName, Permissions: []string{authtypes.Minter, authtypes.Burner}},
	}

	// blocked account addresses
	blockAccAddrs = []string{
		authtypes.FeeCollectorName,
		distrtypes.ModuleName,
		minttypes.ModuleName,
		stakingtypes.BondedPoolName,
		stakingtypes.NotBondedPoolName,
		// We allow the following module accounts to receive funds:
		// govtypes.ModuleName
		// pooltypes.ModuleName
	}
)

var (
	_ runtime.AppI            = (*EthermintApp)(nil)
	_ servertypes.Application = (*EthermintApp)(nil)
)

type GenesisState map[string]json.RawMessage

// var _ server.Application (*EthermintApp)(nil)

// EthermintApp implements an extended ABCI application. It is an application
// that may process transactions through Ethereum's EVM running atop of
// Tendermint consensus.
type EthermintApp struct {
	*baseapp.BaseApp

	// encoding
	cdc                   *codec.LegacyAmino
	appCodec              codec.Codec
	addressCodec          address.Codec
	validatorAddressCodec address.Codec
	txConfig              client.TxConfig
	interfaceRegistry     types.InterfaceRegistry

	invCheckPeriod uint

	pendingTxListeners []ante.PendingTxListener

	// keys to access the substores
	keys    map[string]*storetypes.KVStoreKey
	tkeys   map[string]*storetypes.TransientStoreKey
	memKeys map[string]*storetypes.MemoryStoreKey
	okeys   map[string]*storetypes.ObjectStoreKey

	// keepers
	AccountsKeeper        accounts.Keeper
	AuthKeeper            authkeeper.AccountKeeper
	BankKeeper            bankkeeper.BaseKeeper
	StakingKeeper         *stakingkeeper.Keeper
	SlashingKeeper        slashingkeeper.Keeper
	MintKeeper            *mintkeeper.Keeper
	DistrKeeper           distrkeeper.Keeper
	GovKeeper             govkeeper.Keeper
	UpgradeKeeper         *upgradekeeper.Keeper
	ParamsKeeper          paramskeeper.Keeper
	FeeGrantKeeper        feegrantkeeper.Keeper
	AuthzKeeper           authzkeeper.Keeper
	IBCKeeper             *ibckeeper.Keeper // IBC Keeper must be a pointer in the app, so we can SetRouter on it correctly
	EvidenceKeeper        evidencekeeper.Keeper
	TransferKeeper        ibctransferkeeper.Keeper
	ConsensusParamsKeeper consensusparamkeeper.Keeper
	PoolKeeper            poolkeeper.Keeper

	// Ethermint keepers
	EvmKeeper       *evmkeeper.Keeper
	FeeMarketKeeper feemarketkeeper.Keeper

	// the module manager
	ModuleManager *module.Manager

	// simulation manager
	sm *module.SimulationManager

	// the configurator
	configurator module.Configurator //nolint:staticcheck // SA1019: Configurator is still used in runtime v1.

	UnorderedTxManager *unorderedtx.Manager
}

// NewEthermintApp returns a reference to a new initialized Ethermint application.
func NewEthermintApp(
	logger log.Logger,
	db corestore.KVStoreWithBatch,
	traceStore io.Writer,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *EthermintApp {
	encodingConfig := encoding.MakeConfig()
	appCodec := encodingConfig.Codec
	cdc := encodingConfig.Amino
	txConfig := encodingConfig.TxConfig
	interfaceRegistry := encodingConfig.InterfaceRegistry
	signingCtx := interfaceRegistry.SigningContext()
	txDecoder, err := txdecode.NewDecoder(txdecode.Options{
		SigningContext: signingCtx,
		ProtoCodec:     appCodec,
	})
	if err != nil {
		panic(err)
	}

	eip712.SetEncodingConfig(encodingConfig)

	// NOTE we use custom transaction decoder that supports the sdk.Tx interface instead of sdk.StdTx
	// Setup Mempool and Proposal Handlers
	baseAppOptions = append(baseAppOptions, func(app *baseapp.BaseApp) {
		maxTxs := cast.ToInt(appOpts.Get(server.FlagMempoolMaxTxs))
		if maxTxs <= 0 {
			maxTxs = srvconfig.DefaultMaxTxs
		}
		mempool := mempool.NewPriorityMempool(mempool.PriorityNonceMempoolConfig[int64]{
			TxPriority:      mempool.NewDefaultTxPriority(),
			SignerExtractor: NewEthSignerExtractionAdapter(mempool.NewDefaultSignerExtractionAdapter()),
			MaxTx:           maxTxs,
		})
		handler := baseapp.NewDefaultProposalHandler(mempool, app)

		app.SetMempool(mempool)
		app.SetPrepareProposal(handler.PrepareProposalHandler())
		app.SetProcessProposal(handler.ProcessProposalHandler())
	})
	bApp := baseapp.NewBaseApp(
		appName,
		logger,
		db,
		txConfig.TxDecoder(),
		baseAppOptions...,
	)
	bApp.SetCommitMultiStoreTracer(traceStore)
	bApp.SetVersion(version.Version)
	bApp.SetInterfaceRegistry(interfaceRegistry)
	bApp.SetTxEncoder(txConfig.TxEncoder())
	bApp.SetDisableBlockGasMeter(true)

	keys := storetypes.NewKVStoreKeys(
		// SDK keys
		authtypes.StoreKey, banktypes.StoreKey, stakingtypes.StoreKey,
		minttypes.StoreKey, distrtypes.StoreKey, slashingtypes.StoreKey,
		govtypes.StoreKey, paramstypes.StoreKey, upgradetypes.StoreKey,
		evidencetypes.StoreKey, consensusparamtypes.StoreKey, pooltypes.StoreKey, accounts.StoreKey,
		feegrant.StoreKey, authzkeeper.StoreKey,
		// ibc keys
		ibcexported.StoreKey, ibctransfertypes.StoreKey,
		// ethermint keys
		evmtypes.StoreKey, feemarkettypes.StoreKey,
	)

	// Add the EVM transient store key
	tkeys := storetypes.NewTransientStoreKeys(paramstypes.TStoreKey)
	okeys := storetypes.NewObjectStoreKeys(banktypes.ObjectStoreKey, evmtypes.ObjectStoreKey)

	// load state streaming if enabled
	if err := bApp.RegisterStreamingServices(appOpts, keys); err != nil {
		fmt.Printf("failed to load state streaming: %s", err)
		os.Exit(1)
	}
	invCheckPeriod := cast.ToUint(appOpts.Get(server.FlagInvCheckPeriod))
	app := &EthermintApp{
		BaseApp:               bApp,
		cdc:                   cdc,
		txConfig:              txConfig,
		appCodec:              appCodec,
		addressCodec:          encodingConfig.AddressCodec,
		validatorAddressCodec: encodingConfig.ValidatorAddressCodec,
		interfaceRegistry:     interfaceRegistry,
		invCheckPeriod:        invCheckPeriod,
		keys:                  keys,
		tkeys:                 tkeys,
		okeys:                 okeys,
	}

	// init params keeper and subspaces
	app.ParamsKeeper = initParamsKeeper(appCodec, cdc, keys[paramstypes.StoreKey], tkeys[paramstypes.TStoreKey])

	// get authority address
	authAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	// set the BaseApp's parameter store
	app.ConsensusParamsKeeper = consensusparamkeeper.NewKeeper(
		appCodec,
		runtime.NewEnvironment(runtime.NewKVStoreService(keys[consensusparamtypes.StoreKey]), logger.With(log.ModuleKey, "x/consensus")),
		authAddr,
	)
	bApp.SetParamStore(app.ConsensusParamsKeeper.ParamsStore)

	// set the version modifier
	bApp.SetVersionModifier(consensus.ProvideAppVersionModifier(app.ConsensusParamsKeeper))

	// add keepers
	accountsKeeper, err := accounts.NewKeeper(
		appCodec,
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[accounts.StoreKey]), logger.With(log.ModuleKey, "x/accounts"),
			runtime.EnvWithMsgRouterService(app.MsgServiceRouter()),
			runtime.EnvWithQueryRouterService(app.GRPCQueryRouter()),
		),
		signingCtx.AddressCodec(),
		appCodec.InterfaceRegistry(),
		txDecoder,
		// Lockup account
		accountstd.AddAccount(lockup.CONTINUOUS_LOCKING_ACCOUNT, lockup.NewContinuousLockingAccount),
		accountstd.AddAccount(lockup.PERIODIC_LOCKING_ACCOUNT, lockup.NewPeriodicLockingAccount),
		accountstd.AddAccount(lockup.DELAYED_LOCKING_ACCOUNT, lockup.NewDelayedLockingAccount),
		accountstd.AddAccount(lockup.PERMANENT_LOCKING_ACCOUNT, lockup.NewPermanentLockingAccount),
		accountstd.AddAccount("multisig", multisig.NewAccount),
		// PRODUCTION: add
		baseaccount.NewAccount("base", txConfig.SignModeHandler(), baseaccount.WithSecp256K1PubKey()),
	)
	if err != nil {
		panic(err)
	}
	app.AccountsKeeper = accountsKeeper

	// use custom Ethermint account for contracts
	app.AuthKeeper = authkeeper.NewAccountKeeper(
		runtime.NewEnvironment(runtime.NewKVStoreService(keys[authtypes.StoreKey]), logger.With(log.ModuleKey, "x/auth")),
		appCodec,
		ethermint.ProtoAccount,
		accountsKeeper,
		GetMaccPerms(),
		signingCtx.AddressCodec(),
		sdk.Bech32MainPrefix,
		authAddr,
	)

	app.BankKeeper = bankkeeper.NewBaseKeeper(
		runtime.NewEnvironment(runtime.NewKVStoreService(keys[banktypes.StoreKey]), logger.With(log.ModuleKey, "x/bank")),
		appCodec,
		okeys[banktypes.ObjectStoreKey],
		app.AuthKeeper,
		app.BlockedAddresses(),
		authAddr,
	)

	// optional: enable sign mode textual by overwriting the default tx config (after setting the bank keeper)
	enabledSignModes := slices.Clone(authtx.DefaultSignModes)
	enabledSignModes = append(enabledSignModes, sigtypes.SignMode_SIGN_MODE_TEXTUAL)
	txConfigOpts := authtx.ConfigOptions{
		EnabledSignModes:           enabledSignModes,
		TextualCoinMetadataQueryFn: txmodule.NewBankKeeperCoinMetadataQueryFn(app.BankKeeper),
		SigningOptions: &signing.Options{
			AddressCodec:          signingCtx.AddressCodec(),
			ValidatorAddressCodec: signingCtx.ValidatorAddressCodec(),
		},
	}
	txConfig, err = authtx.NewTxConfigWithOptions(
		appCodec,
		txConfigOpts,
	)
	if err != nil {
		panic(err)
	}
	app.txConfig = txConfig
	cometService := runtime.NewContextAwareCometInfoService()
	app.StakingKeeper = stakingkeeper.NewKeeper(
		appCodec,
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[stakingtypes.StoreKey]),
			logger.With(log.ModuleKey, "x/staking"),
			runtime.EnvWithMsgRouterService(app.MsgServiceRouter()),
			runtime.EnvWithQueryRouterService(app.GRPCQueryRouter())),
		app.AuthKeeper,
		app.BankKeeper,
		app.ConsensusParamsKeeper,
		authAddr,
		signingCtx.ValidatorAddressCodec(),
		authcodec.NewBech32Codec(sdk.Bech32PrefixConsAddr),
		cometService,
	)
	app.MintKeeper = mintkeeper.NewKeeper(
		appCodec,
		runtime.NewEnvironment(runtime.NewKVStoreService(keys[minttypes.StoreKey]), logger.With(log.ModuleKey, "x/mint")),
		app.AuthKeeper,
		app.BankKeeper,
		authtypes.FeeCollectorName,
		authAddr,
	)
	if err := app.MintKeeper.SetMintFn(
		mintkeeper.DefaultMintFn(minttypes.DefaultInflationCalculationFn, app.StakingKeeper, app.MintKeeper),
	); err != nil {
		panic(err)
	}
	app.PoolKeeper = poolkeeper.NewKeeper(
		appCodec,
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[pooltypes.StoreKey]), logger.With(log.ModuleKey, "x/protocolpool"),
		),
		app.AuthKeeper,
		app.BankKeeper,
		authAddr,
	)
	app.DistrKeeper = distrkeeper.NewKeeper(
		appCodec,
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[distrtypes.StoreKey]), logger.With(log.ModuleKey, "x/distribution"),
		),
		app.AuthKeeper,
		app.BankKeeper,
		app.StakingKeeper,
		cometService,
		authtypes.FeeCollectorName,
		authAddr,
	)
	app.SlashingKeeper = slashingkeeper.NewKeeper(
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[slashingtypes.StoreKey]), logger.With(log.ModuleKey, "x/slashing"),
		),
		appCodec, app.LegacyAmino(),
		app.StakingKeeper,
		authAddr,
	)
	app.FeeGrantKeeper = feegrantkeeper.NewKeeper(
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[feegrant.StoreKey]), logger.With(log.ModuleKey, "x/feegrant"),
		),
		appCodec,
		app.AuthKeeper,
	)
	// get skipUpgradeHeights from the app options
	skipUpgradeHeights := map[int64]bool{}
	for _, h := range cast.ToIntSlice(appOpts.Get(server.FlagUnsafeSkipUpgrades)) {
		skipUpgradeHeights[int64(h)] = true
	}
	homePath := cast.ToString(appOpts.Get(flags.FlagHome))
	if homePath == "" {
		homePath = DefaultNodeHome
	}
	// set the governance module account as the authority for conducting upgrades
	app.UpgradeKeeper = upgradekeeper.NewKeeper(
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[upgradetypes.StoreKey]), logger.With(log.ModuleKey, "x/upgrade"),
			runtime.EnvWithMsgRouterService(app.MsgServiceRouter()),
			runtime.EnvWithQueryRouterService(app.GRPCQueryRouter()),
		),
		skipUpgradeHeights,
		appCodec,
		homePath,
		app.BaseApp,
		authAddr,
		app.ConsensusParamsKeeper,
	)

	// register the staking hooks
	app.StakingKeeper.SetHooks(
		stakingtypes.NewMultiStakingHooks(app.DistrKeeper.Hooks(),
			app.SlashingKeeper.Hooks(),
		),
	)
	app.AuthzKeeper = authzkeeper.NewKeeper(
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[authzkeeper.StoreKey]), logger.With(log.ModuleKey, "x/authz"),
			runtime.EnvWithMsgRouterService(app.MsgServiceRouter()),
			runtime.EnvWithQueryRouterService(app.GRPCQueryRouter()),
		),
		appCodec,
		app.AuthKeeper,
	)

	// Create IBC Keeper
	app.IBCKeeper = ibckeeper.NewKeeper(
		appCodec,
		runtime.NewKVStoreService(keys[ibcexported.StoreKey]),
		app.GetSubspace(ibcexported.ModuleName),
		app.UpgradeKeeper,
		// scopedIBCKeeper,
		authAddr,
	)

	tracer := cast.ToString(appOpts.Get(srvflags.EVMTracer))

	// Create Ethermint keepers
	feeMarketSs := app.GetSubspace(feemarkettypes.ModuleName)
	app.FeeMarketKeeper = feemarketkeeper.NewKeeper(
		appCodec,
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[feemarkettypes.StoreKey]), logger.With(log.ModuleKey, "x/feemarket"),
			runtime.EnvWithMsgRouterService(app.MsgServiceRouter()),
			runtime.EnvWithQueryRouterService(app.GRPCQueryRouter()),
		),
		authtypes.NewModuleAddress(govtypes.ModuleName),
		feeMarketSs,
	)

	// Set authority to x/gov module account to only expect the module account to update params
	evmSs := app.GetSubspace(evmtypes.ModuleName)
	app.EvmKeeper = evmkeeper.NewKeeper(
		appCodec,
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[evmtypes.StoreKey]), logger.With(log.ModuleKey, "x/evm"),
			runtime.EnvWithMsgRouterService(app.MsgServiceRouter()),
			runtime.EnvWithQueryRouterService(app.GRPCQueryRouter()),
		),
		keys[evmtypes.StoreKey], okeys[evmtypes.ObjectStoreKey], authtypes.NewModuleAddress(govtypes.ModuleName),
		app.AuthKeeper, app.BankKeeper, app.StakingKeeper, app.FeeMarketKeeper,
		tracer,
		evmSs,
		nil,
	)

	// register the proposal types
	govRouter := govv1beta1.NewRouter()
	govRouter.AddRoute(govtypes.RouterKey, govv1beta1.ProposalHandler).
		AddRoute(paramproposal.RouterKey, params.NewParamChangeProposalHandler(app.ParamsKeeper))
	govConfig := govkeeper.DefaultConfig()
	/*
		Example of setting gov params:
		govConfig.MaxMetadataLen = 10000
	*/
	govKeeper := govkeeper.NewKeeper(
		appCodec,
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[govtypes.StoreKey]), logger.With(log.ModuleKey, "x/gov"),
			runtime.EnvWithMsgRouterService(app.MsgServiceRouter()),
			runtime.EnvWithQueryRouterService(app.GRPCQueryRouter()),
		),
		app.AuthKeeper,
		app.BankKeeper,
		app.StakingKeeper,
		app.PoolKeeper,
		govConfig,
		authAddr,
	)

	// Set legacy router for backwards compatibility with gov v1beta1
	govKeeper.SetLegacyRouter(govRouter)

	app.GovKeeper = *govKeeper.SetHooks(
		govtypes.NewMultiGovHooks(
		// register the governance hooks
		),
	)

	// Create Transfer Keepers
	app.TransferKeeper = ibctransferkeeper.NewKeeper(
		appCodec,
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[ibctransfertypes.StoreKey]),
			logger.With(log.ModuleKey, fmt.Sprintf("x/%s-%s", ibcexported.ModuleName, ibctransfertypes.ModuleName)),
		),
		app.GetSubspace(ibctransfertypes.ModuleName),
		app.IBCKeeper.ChannelKeeper,
		app.IBCKeeper.ChannelKeeper,
		app.AuthKeeper,
		app.BankKeeper,
		authAddr,
	)
	transferModule := transfer.NewAppModule(appCodec, app.TransferKeeper)
	transferIBCModule := transfer.NewIBCModule(app.TransferKeeper)

	// Create static IBC router, add transfer route, then set and seal it
	ibcRouter := porttypes.NewRouter()
	ibcRouter.AddRoute(ibctransfertypes.ModuleName, transferIBCModule)
	app.IBCKeeper.SetRouter(ibcRouter)
	clientKeeper := app.IBCKeeper.ClientKeeper
	storeProvider := clientKeeper.GetStoreProvider()

	tmLightClientModule := ibctm.NewLightClientModule(appCodec, storeProvider)
	clientKeeper.AddRoute(ibctm.ModuleName, &tmLightClientModule)

	// create evidence keeper with router
	evidenceKeeper := evidencekeeper.NewKeeper(
		appCodec,
		runtime.NewEnvironment(
			runtime.NewKVStoreService(keys[evidencetypes.StoreKey]), logger.With(log.ModuleKey, "x/evidence"),
			runtime.EnvWithMsgRouterService(app.MsgServiceRouter()),
			runtime.EnvWithQueryRouterService(app.GRPCQueryRouter()),
		),
		app.StakingKeeper,
		app.SlashingKeeper,
		app.ConsensusParamsKeeper,
		app.AuthKeeper.AddressCodec(),
	)

	// If evidence needs to be handled for the app, set routes in router here and seal
	app.EvidenceKeeper = *evidenceKeeper

	/****  Module Options ****/

	// NOTE: Any module instantiated in the module manager that is later modified
	// must be passed by reference here.
	app.ModuleManager = module.NewManager(
		// SDK app modules
		genutil.NewAppModule(appCodec, app.AuthKeeper, app.StakingKeeper, app, txConfig, genutiltypes.DefaultMessageValidator),
		accounts.NewAppModule(appCodec, app.AccountsKeeper),
		auth.NewAppModule(appCodec, app.AuthKeeper, app.AccountsKeeper, authsims.RandomGenesisAccounts, nil),
		vesting.NewAppModule(app.AuthKeeper, app.BankKeeper),
		bank.NewAppModule(appCodec, app.BankKeeper, app.AuthKeeper),
		// capability.NewAppModule(appCodec, *app.CapabilityKeeper, false),
		feegrantmodule.NewAppModule(appCodec, app.FeeGrantKeeper, app.interfaceRegistry),
		gov.NewAppModule(appCodec, &app.GovKeeper, app.AuthKeeper, app.BankKeeper, app.PoolKeeper),
		mint.NewAppModule(appCodec, app.MintKeeper, app.AuthKeeper),
		slashing.NewAppModule(
			appCodec,
			app.SlashingKeeper,
			app.AuthKeeper,
			app.BankKeeper,
			app.StakingKeeper,
			app.interfaceRegistry,
			cometService,
		),
		distr.NewAppModule(appCodec, app.DistrKeeper, app.StakingKeeper),
		staking.NewAppModule(appCodec, app.StakingKeeper),
		upgrade.NewAppModule(app.UpgradeKeeper),
		evidence.NewAppModule(appCodec, app.EvidenceKeeper, cometService),
		params.NewAppModule(app.ParamsKeeper),
		authzmodule.NewAppModule(appCodec, app.AuthzKeeper, app.interfaceRegistry),
		consensus.NewAppModule(appCodec, app.ConsensusParamsKeeper),
		protocolpool.NewAppModule(appCodec, app.PoolKeeper, app.AuthKeeper, app.BankKeeper),
		// ibc modules
		ibc.NewAppModule(appCodec, app.IBCKeeper),
		// IBC light clients
		ibctm.NewAppModule(tmLightClientModule),
		transferModule,
		// Ethermint app modules
		feemarket.NewAppModule(appCodec, app.FeeMarketKeeper, feeMarketSs),
		evm.NewAppModule(appCodec, app.EvmKeeper, app.AuthKeeper, app.BankKeeper, app.AuthKeeper.Accounts, evmSs),
	)

	app.ModuleManager.RegisterLegacyAminoCodec(cdc)
	app.ModuleManager.RegisterInterfaces(interfaceRegistry)

	app.ModuleManager.SetOrderPreBlockers(
		upgradetypes.ModuleName,
	)

	// During begin block slashing happens after distr.BeginBlocker so that
	// there is nothing left over in the validator fee pool, so as to keep the
	// CanWithdrawInvariant invariant.
	// NOTE: upgrade module must go first to handle software upgrades.
	// NOTE: staking module is required if HistoricalEntries param > 0
	// NOTE: capability module's beginblocker must come before any modules using capabilities (e.g. IBC)
	app.ModuleManager.SetOrderBeginBlockers(
		// capabilitytypes.ModuleName,
		feemarkettypes.ModuleName,
		evmtypes.ModuleName,
		minttypes.ModuleName,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		evidencetypes.ModuleName,
		stakingtypes.ModuleName,
		ibcexported.ModuleName,
		// no-op modules
		ibctransfertypes.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		govtypes.ModuleName,
		genutiltypes.ModuleName,
		authz.ModuleName,
		feegrant.ModuleName,
		paramstypes.ModuleName,
		vestingtypes.ModuleName,
		consensusparamtypes.ModuleName,
		pooltypes.ModuleName,
	)

	// NOTE: fee market module must go last in order to retrieve the block gas used.
	app.ModuleManager.SetOrderEndBlockers(
		banktypes.ModuleName,
		govtypes.ModuleName,
		stakingtypes.ModuleName,
		evmtypes.ModuleName,
		feemarkettypes.ModuleName,
		// no-op modules
		// capabilitytypes.ModuleName,
		ibcexported.ModuleName,
		ibctransfertypes.ModuleName,
		authtypes.ModuleName,
		distrtypes.ModuleName,
		slashingtypes.ModuleName,
		minttypes.ModuleName,
		genutiltypes.ModuleName,
		evidencetypes.ModuleName,
		authz.ModuleName,
		feegrant.ModuleName,
		paramstypes.ModuleName,
		vestingtypes.ModuleName,
		consensusparamtypes.ModuleName,
		pooltypes.ModuleName,
	)

	// NOTE: The genutils module must occur after staking so that pools are
	// properly initialized with tokens from genesis accounts.
	// NOTE: The genutils module must also occur after auth so that it can access the params from auth.
	// NOTE: Capability module must occur first so that it can initialize any capabilities
	// so that other modules that want to create or claim capabilities afterwards in InitChain
	// can do so safely.
	genesisModuleOrder := []string{
		// SDK modules
		// capabilitytypes.ModuleName,
		authtypes.ModuleName,
		banktypes.ModuleName,
		distrtypes.ModuleName,
		stakingtypes.ModuleName,
		slashingtypes.ModuleName,
		govtypes.ModuleName,
		minttypes.ModuleName,
		ibcexported.ModuleName,
		// evm module denomination is used by the feemarket module, in AnteHandle
		evmtypes.ModuleName,
		// NOTE: feemarket need to be initialized before genutil module:
		// gentx transactions use MinGasPriceDecorator.AnteHandle
		feemarkettypes.ModuleName,
		genutiltypes.ModuleName,
		evidencetypes.ModuleName,
		ibctransfertypes.ModuleName,
		authz.ModuleName,
		feegrant.ModuleName,
		paramstypes.ModuleName,
		upgradetypes.ModuleName,
		vestingtypes.ModuleName,
		consensusparamtypes.ModuleName,
		pooltypes.ModuleName,
		accounts.ModuleName,
	}
	app.ModuleManager.SetOrderInitGenesis(genesisModuleOrder...)
	app.ModuleManager.SetOrderExportGenesis(genesisModuleOrder...)

	// Uncomment if you want to set a custom migration order here.
	// app.ModuleManager.SetOrderMigrations(custom order)

	app.configurator = module.NewConfigurator(app.appCodec, app.MsgServiceRouter(), app.GRPCQueryRouter())
	if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
		panic(err)
	}

	// RegisterUpgradeHandlers is used for registering any on-chain upgrades.
	// Make sure it's called after `app.ModuleManager` and `app.configurator` are set.
	app.RegisterUpgradeHandlers()

	// add test gRPC service for testing gRPC queries in isolation
	// testdata.RegisterTestServiceServer(app.GRPCQueryRouter(), testdata.TestServiceImpl{})

	// create the simulation manager and define the order of the modules for deterministic simulations
	//
	// NOTE: this is not required apps that don't use the simulator for fuzz testing
	// transactions
	overrideModules := map[string]module.AppModuleSimulation{
		authtypes.ModuleName: auth.NewAppModule(app.appCodec, app.AuthKeeper, app.AccountsKeeper, authsims.RandomGenesisAccounts, nil),
	}
	app.sm = module.NewSimulationManagerFromAppModules(app.ModuleManager.Modules, overrideModules)

	// create, start, and load the unordered tx manager
	utxDataDir := filepath.Join(homePath, "data")
	app.UnorderedTxManager = unorderedtx.NewManager(utxDataDir)
	app.UnorderedTxManager.Start()

	if err := app.UnorderedTxManager.OnInit(); err != nil {
		panic(fmt.Errorf("failed to initialize unordered tx manager: %w", err))
	}

	// register custom snapshot extensions (if any)
	if manager := app.SnapshotManager(); manager != nil {
		err := manager.RegisterExtensions(
			unorderedtx.NewSnapshotter(app.UnorderedTxManager),
		)
		if err != nil {
			panic(fmt.Errorf("failed to register snapshot extension: %w", err))
		}
	}

	autocliv1.RegisterQueryServer(app.GRPCQueryRouter(), runtimeservices.NewAutoCLIQueryService(app.ModuleManager.Modules))

	reflectionSvc, err := runtimeservices.NewReflectionService()
	if err != nil {
		panic(err)
	}
	reflectionv1.RegisterReflectionServiceServer(app.GRPCQueryRouter(), reflectionSvc)

	app.sm.RegisterStoreDecoders()

	// initialize stores
	app.MountKVStores(keys)
	app.MountTransientStores(tkeys)
	app.MountObjectStores(okeys)

	// initialize BaseApp
	app.SetInitChainer(app.InitChainer)
	app.SetPreBlocker(app.PreBlocker)
	app.SetBeginBlocker(app.BeginBlocker)
	app.SetEndBlocker(app.EndBlocker)
	app.setAnteHandler(txConfig, cast.ToUint64(appOpts.Get(srvflags.EVMMaxTxGasWanted)), logger)
	// In v0.46, the SDK introduces _postHandlers_. PostHandlers are like
	// antehandlers, but are run _after_ the `runMsgs` execution. They are also
	// defined as a chain, and have the same signature as antehandlers.
	//
	// In baseapp, postHandlers are run in the same store branch as `runMsgs`,
	// meaning that both `runMsgs` and `postHandler` state will be committed if
	// both are successful, and both will be reverted if any of the two fails.
	//
	// The SDK exposes a default empty postHandlers chain.
	//
	// Please note that changing any of the anteHandler or postHandler chain is
	// likely to be a state-machine breaking change, which needs a coordinated
	// upgrade.
	app.setPostHandler()

	// At startup, after all modules have been registered, check that all prot
	// annotations are correct.
	protoFiles, err := proto.MergedRegistry()
	if err != nil {
		panic(err)
	}
	err = msgservice.ValidateProtoAnnotations(protoFiles)
	if err != nil {
		// Once we switch to using protoreflect-based antehandlers, we might
		// want to panic here instead of logging a warning.
		fmt.Fprintln(os.Stderr, err.Error())
	}
	if loadLatest {
		if err := app.LoadLatestVersion(); err != nil {
			panic(fmt.Errorf("error loading last version: %w", err))
		}
	}

	executor := cast.ToString(appOpts.Get(srvflags.EVMBlockExecutor))
	switch executor {
	case srvconfig.BlockExecutorBlockSTM:
		sdk.SetAddrCacheEnabled(false)
		workers := cast.ToInt(appOpts.Get(srvflags.EVMBlockSTMWorkers))
		preEstimate := cast.ToBool(appOpts.Get(srvflags.EVMBlockSTMPreEstimate))
		app.SetTxExecutor(STMTxExecutor(app.GetStoreKeys(), workers, preEstimate, app.EvmKeeper, txConfig.TxDecoder()))
	case "", srvconfig.BlockExecutorSequential:
		app.SetTxExecutor(DefaultTxExecutor)
	default:
		panic(fmt.Errorf("unknown EVM block executor: %s", executor))
	}

	return app
}

// use Ethermint's custom AnteHandler
func (app *EthermintApp) setAnteHandler(txConfig client.TxConfig, maxGasWanted uint64, logger log.Logger) {
	anteHandler, err := ante.NewAnteHandler(ante.HandlerOptions{
		Environment: runtime.NewEnvironment(
			nil,
			logger,
			runtime.EnvWithMsgRouterService(app.MsgServiceRouter()),
			runtime.EnvWithQueryRouterService(app.GRPCQueryRouter()),
		), // nil is set as the kvstoreservice to avoid module access
		ConsensusKeeper:          app.ConsensusParamsKeeper,
		AccountKeeper:            app.AuthKeeper,
		AccountAbstractionKeeper: app.AccountsKeeper,
		BankKeeper:               app.BankKeeper,
		FeeMarketKeeper:          app.FeeMarketKeeper,
		EvmKeeper:                app.EvmKeeper,
		FeegrantKeeper:           app.FeeGrantKeeper,
		SignModeHandler:          txConfig.SignModeHandler(),
		SigGasConsumer:           ante.DefaultSigVerificationGasConsumer,
		IBCKeeper:                app.IBCKeeper,
		MaxTxGasWanted:           maxGasWanted,
		ExtensionOptionChecker:   ethermint.HasDynamicFeeExtensionOption,
		DynamicFeeChecker:        true,
		DisabledAuthzMsgs: []string{
			sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
			sdk.MsgTypeURL(&vestingtypes.BaseVestingAccount{}),
			sdk.MsgTypeURL(&vestingtypes.PermanentLockedAccount{}),
			sdk.MsgTypeURL(&vestingtypes.PeriodicVestingAccount{}),
		},
		PendingTxListener:  app.onPendingTx,
		UnorderedTxManager: app.UnorderedTxManager,
	})
	if err != nil {
		panic(err)
	}

	app.SetAnteHandler(anteHandler)
}

func (app *EthermintApp) onPendingTx(hash common.Hash) {
	for _, listener := range app.pendingTxListeners {
		listener(hash)
	}
}

func (app *EthermintApp) setPostHandler() {
	postHandler, err := posthandler.NewPostHandler(
		posthandler.HandlerOptions{},
	)
	if err != nil {
		panic(err)
	}

	app.SetPostHandler(postHandler)
}

// Close closes all necessary application resources.
// It implements servertypes.Application.
func (app *EthermintApp) Close() error {
	if err := app.BaseApp.Close(); err != nil {
		return err
	}

	return app.UnorderedTxManager.Close()
}

// Name returns the name of the App
func (app *EthermintApp) Name() string { return app.BaseApp.Name() }

// PreBlocker updates every pre begin block
func (app *EthermintApp) PreBlocker(ctx sdk.Context, _ *abci.FinalizeBlockRequest) error {
	app.UnorderedTxManager.OnNewBlock(ctx.BlockTime())
	return app.ModuleManager.PreBlock(ctx)
}

// BeginBlocker updates every begin block
func (app *EthermintApp) BeginBlocker(ctx sdk.Context) (sdk.BeginBlock, error) {
	return app.ModuleManager.BeginBlock(ctx)
}

// EndBlocker updates every end block
func (app *EthermintApp) EndBlocker(ctx sdk.Context) (sdk.EndBlock, error) {
	return app.ModuleManager.EndBlock(ctx)
}

func (app *EthermintApp) Configurator() module.Configurator { //nolint:staticcheck // SA1019: Configurator is still used in runtime v1.
	return app.configurator
}

// InitChainer updates at chain initialization
func (app *EthermintApp) InitChainer(ctx sdk.Context, req *abci.InitChainRequest) (*abci.InitChainResponse, error) {
	var genesisState GenesisState
	err := json.Unmarshal(req.AppStateBytes, &genesisState)
	if err != nil {
		return nil, err
	}
	err = app.UpgradeKeeper.SetModuleVersionMap(ctx, app.ModuleManager.GetVersionMap())
	if err != nil {
		return nil, err
	}
	return app.ModuleManager.InitGenesis(ctx, genesisState)
}

// LoadHeight loads state at a particular height
func (app *EthermintApp) LoadHeight(height int64) error {
	return app.LoadVersion(height)
}

// BlockedAddresses returns all the app's blocked account addresses.
// This function takes an address.Codec parameter to maintain compatibility
// with the signature of the same function in appV1.
func (app *EthermintApp) BlockedAddresses() map[string]bool {
	result := make(map[string]bool)

	if len(blockAccAddrs) > 0 {
		for _, addr := range blockAccAddrs {
			result[addr] = true
		}
	} else {
		for addr := range GetMaccPerms() {
			result[addr] = true
		}
	}

	return result
}

// LegacyAmino returns EthermintApp's amino codec.
//
// NOTE: This is solely to be used for testing purposes as it may be desirable
// for modules to register their own custom testing types.
func (app *EthermintApp) LegacyAmino() *codec.LegacyAmino {
	return app.cdc
}

// AppCodec returns EthermintApp's app codec.
//
// NOTE: This is solely to be used for testing purposes as it may be desirable
// for modules to register their own custom testing types.
func (app *EthermintApp) AppCodec() codec.Codec {
	return app.appCodec
}

func (app *EthermintApp) AppAddressCodec() address.Codec {
	return app.addressCodec
}

func (app *EthermintApp) AppValidatorAddressCodec() address.Codec {
	return app.validatorAddressCodec
}

// InterfaceRegistry returns EthermintApp's InterfaceRegistry
func (app *EthermintApp) InterfaceRegistry() types.InterfaceRegistry {
	return app.interfaceRegistry
}

// DefaultGenesis returns a default genesis from the registered AppModuleBasic's.
func (app *EthermintApp) DefaultGenesis() map[string]json.RawMessage {
	return app.ModuleManager.DefaultGenesis()
}

func (app *EthermintApp) TxConfig() client.TxConfig {
	return app.txConfig
}

func (app *EthermintApp) EncodingConfig() ethermint.EncodingConfig {
	return ethermint.EncodingConfig{
		InterfaceRegistry:     app.InterfaceRegistry(),
		Codec:                 app.AppCodec(),
		AddressCodec:          app.AppAddressCodec(),
		ValidatorAddressCodec: app.AppValidatorAddressCodec(),
		TxConfig:              app.TxConfig(),
		Amino:                 app.LegacyAmino(),
	}
}

// AutoCliOpts returns the autocli options for the app.
func (app *EthermintApp) AutoCliOpts() autocli.AppOptions {
	return autocli.AppOptions{
		Modules:               app.ModuleManager.Modules,
		ModuleOptions:         runtimeservices.ExtractAutoCLIOptions(app.ModuleManager.Modules),
		AddressCodec:          authcodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix()),
		ValidatorAddressCodec: authcodec.NewBech32Codec(sdk.GetConfig().GetBech32ValidatorAddrPrefix()),
		ConsensusAddressCodec: authcodec.NewBech32Codec(sdk.GetConfig().GetBech32ConsensusAddrPrefix()),
	}
}

// GetKey returns the KVStoreKey for the provided store key.
//
// NOTE: This is solely to be used for testing purposes.
func (app *EthermintApp) GetKey(storeKey string) *storetypes.KVStoreKey {
	return app.keys[storeKey]
}

// GetTKey returns the TransientStoreKey for the provided store key.
//
// NOTE: This is solely to be used for testing purposes.
func (app *EthermintApp) GetTKey(storeKey string) *storetypes.TransientStoreKey {
	return app.tkeys[storeKey]
}

// GetMemKey returns the MemStoreKey for the provided mem key.
//
// NOTE: This is solely used for testing purposes.
func (app *EthermintApp) GetMemKey(storeKey string) *storetypes.MemoryStoreKey {
	return app.memKeys[storeKey]
}

// GetStoreKeys returns all the stored store keys.
func (app *EthermintApp) GetStoreKeys() []storetypes.StoreKey {
	keys := make([]storetypes.StoreKey, 0, len(app.keys))
	for _, key := range app.keys {
		keys = append(keys, key)
	}
	for _, key := range app.tkeys {
		keys = append(keys, key)
	}
	for _, key := range app.memKeys {
		keys = append(keys, key)
	}
	for _, key := range app.okeys {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool { return keys[i].Name() < keys[j].Name() })
	return keys
}

// GetSubspace returns a param subspace for a given module name.
//
// NOTE: This is solely to be used for testing purposes.
func (app *EthermintApp) GetSubspace(moduleName string) paramstypes.Subspace {
	subspace, _ := app.ParamsKeeper.GetSubspace(moduleName)
	return subspace
}

// SimulationManager implements the SimulationApp interface
func (app *EthermintApp) SimulationManager() *module.SimulationManager {
	return app.sm
}

// RegisterAPIRoutes registers all application module routes with the provided
// API server.
func (app *EthermintApp) RegisterAPIRoutes(apiSvr *api.Server, apiConfig config.APIConfig) {
	clientCtx := apiSvr.ClientCtx
	app.EvmKeeper.WithChainIDString(clientCtx.ChainID)
	// Register new tx routes from grpc-gateway.
	authtx.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	// Register new tendermint queries routes from grpc-gateway.
	cmtservice.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
	// Register node gRPC service for grpc-gateway.
	node.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)

	// Register grpc-gateway routes for all modules.
	app.ModuleManager.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)

	// register swagger API from root so that other applications can override easily
	if apiConfig.Swagger {
		RegisterSwaggerAPI(clientCtx, apiSvr.Router)
	}
}

// RegisterTxService implements the Application.RegisterTxService method.
func (app *EthermintApp) RegisterTxService(clientCtx client.Context) {
	authtx.RegisterTxService(app.BaseApp.GRPCQueryRouter(), clientCtx, app.BaseApp.Simulate, app.interfaceRegistry)
}

// RegisterTendermintService implements the Application.RegisterTendermintService method.
func (app *EthermintApp) RegisterTendermintService(clientCtx client.Context) {
	cmtservice.RegisterTendermintService(
		clientCtx,
		app.BaseApp.GRPCQueryRouter(),
		app.interfaceRegistry,
		app.Query,
	)
}

func (app *EthermintApp) RegisterNodeService(clientCtx client.Context, cfg config.Config) {
	node.RegisterNodeService(clientCtx, app.GRPCQueryRouter(), cfg)
}

// GetStoreKey is used by unit test
func (app *EthermintApp) GetStoreKey(name string) storetypes.StoreKey {
	key, ok := app.keys[name]
	if ok {
		return key
	}
	tkey, ok := app.tkeys[name]
	if ok {
		return tkey
	}
	mkey, ok := app.memKeys[name]
	if ok {
		return mkey
	}
	return app.okeys[name]
}

// RegisterPendingTxListener is used by json-rpc server to listen to pending transactions callback.
func (app *EthermintApp) RegisterPendingTxListener(listener ante.PendingTxListener) {
	app.pendingTxListeners = append(app.pendingTxListeners, listener)
}

// ValidatorKeyProvider returns a function that generates a validator key
// Supported key types are those supported by Comet: ed25519, secp256k1, bls12-381
func (*EthermintApp) ValidatorKeyProvider() runtime.KeyGenF {
	return func() (cmtcrypto.PrivKey, error) {
		return cmted25519.GenPrivKey(), nil
	}
}

// RegisterSwaggerAPI registers swagger route with API Server
func RegisterSwaggerAPI(_ client.Context, rtr *mux.Router) {
	root, err := fs.Sub(docs.SwaggerUI, "swagger-ui")
	if err != nil {
		panic(err)
	}

	staticServer := http.FileServer(http.FS(root))
	rtr.PathPrefix("/swagger/").Handler(http.StripPrefix("/swagger/", staticServer))
}

// GetMaccPerms returns a copy of the module account permissions
//
// NOTE: This is solely to be used for testing purposes.
func GetMaccPerms() map[string][]string {
	dup := make(map[string][]string)
	for _, perms := range moduleAccPerms {
		dup[perms.Account] = perms.Permissions
	}

	return dup
}

// initParamsKeeper init params keeper and its subspaces
func initParamsKeeper(appCodec codec.BinaryCodec, legacyAmino *codec.LegacyAmino, key, tkey storetypes.StoreKey) paramskeeper.Keeper {
	paramsKeeper := paramskeeper.NewKeeper(appCodec, legacyAmino, key, tkey)
	// register the key tables for legacy param subspaces
	keyTable := ibcclienttypes.ParamKeyTable()
	keyTable.RegisterParamSet(&ibcconnectiontypes.Params{})
	paramsKeeper.Subspace(ibcexported.ModuleName).WithKeyTable(keyTable)
	paramsKeeper.Subspace(ibctransfertypes.ModuleName).WithKeyTable(ibctransfertypes.ParamKeyTable())
	// ethermint subspaces
	paramsKeeper.Subspace(evmtypes.ModuleName).WithKeyTable(v0evmtypes.ParamKeyTable()) //nolint: staticcheck
	paramsKeeper.Subspace(feemarkettypes.ModuleName).WithKeyTable(feemarkettypes.ParamKeyTable())
	return paramsKeeper
}
