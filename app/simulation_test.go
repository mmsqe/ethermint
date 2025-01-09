//go:build sims

package app_test

// TODO: COsmos SDK fix for the simulator issue for custom keys
import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"testing"
	"time"

	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"cosmossdk.io/simapp"
	"cosmossdk.io/store"
	authzkeeper "cosmossdk.io/x/authz/keeper"
	"cosmossdk.io/x/feegrant"
	slashingtypes "cosmossdk.io/x/slashing/types"
	stakingtypes "cosmossdk.io/x/staking/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/api/cometbft/types/v1"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/simsx"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"
	simcli "github.com/cosmos/cosmos-sdk/x/simulation/client/cli"
	"github.com/evmos/ethermint/app"
	"github.com/evmos/ethermint/app/ante"
	"github.com/evmos/ethermint/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var FlagEnableStreamingValue bool

func init() {
	simcli.GetSimulatorFlags()
	flag.BoolVar(&FlagEnableStreamingValue, "EnableStreaming", false, "Enable streaming service")
}

const (
	appName        = "ethermintd"
	SimAppChainID  = "simulation_777-1"
	SimBlockMaxGas = 815000000
)

// interBlockCacheOpt returns a BaseApp option function that sets the persistent
// inter-block write-through cache.
func interBlockCacheOpt() func(*baseapp.BaseApp) {
	return baseapp.SetInterBlockCache(store.NewCommitKVStoreCacheManager())
}

func setupStateFactory(app *app.EthermintApp) simsx.SimStateFactory {
	return simsx.SimStateFactory{
		Codec:         app.AppCodec(),
		AppStateFn:    testutil.StateFn(app),
		BlockedAddr:   app.BlockedAddresses(),
		AccountSource: app.AuthKeeper,
		BalanceSource: app.BankKeeper,
	}
}

func TestFullAppSimulation(t *testing.T) {
	config := simcli.NewConfigFromFlags()
	config.ChainID = SimAppChainID
	config.BlockMaxGas = SimBlockMaxGas
	simsx.RunWithSeedAndRandAcc(
		t,
		config,
		NewSimApp,
		setupStateFactory,
		config.Seed,
		config.FuzzSeed,
		testutil.RandomAccounts,
	)
}

var (
	exportAllModules       []string
	exportWithValidatorSet []string
)

func NewSimApp(
	logger log.Logger,
	db corestore.KVStoreWithBatch,
	traceStore io.Writer,
	loadLatest bool,
	appOpts servertypes.AppOptions,
	baseAppOptions ...func(*baseapp.BaseApp),
) *app.EthermintApp {
	appOptions := make(simtestutil.AppOptionsMap, 0)
	appOptions[flags.FlagHome] = app.DefaultNodeHome
	appOptions[server.FlagInvCheckPeriod] = simcli.FlagPeriodValue
	app := app.NewEthermintApp(logger, db, nil, false, appOptions, baseAppOptions...)
	// disable feemarket on native tx
	anteHandler, err := ante.NewAnteHandler(ante.HandlerOptions{
		Environment: runtime.NewEnvironment(
			nil,
			logger,
		), // nil is set as the kvstoreservice to avoid module access
		ConsensusKeeper:          app.ConsensusParamsKeeper,
		AccountKeeper:            app.AuthKeeper,
		AccountAbstractionKeeper: app.AccountsKeeper,
		BankKeeper:               app.BankKeeper,
		SignModeHandler:          app.TxConfig().SignModeHandler(),
		FeegrantKeeper:           app.FeeGrantKeeper,
		SigGasConsumer:           ante.DefaultSigVerificationGasConsumer,
		IBCKeeper:                app.IBCKeeper,
		EvmKeeper:                app.EvmKeeper,
		FeeMarketKeeper:          app.FeeMarketKeeper,
		MaxTxGasWanted:           0,
	})
	if err != nil {
		panic(err)
	}
	app.SetAnteHandler(anteHandler)
	if err := app.LoadLatestVersion(); err != nil {
		panic(err)
	}
	return app
}

func TestAppImportExport(t *testing.T) {
	return
	config := simcli.NewConfigFromFlags()
	config.ChainID = SimAppChainID
	config.BlockMaxGas = SimBlockMaxGas
	simsx.RunWithSeedAndRandAcc(
		t,
		config,
		NewSimApp,
		setupStateFactory,
		config.Seed,
		config.FuzzSeed,
		testutil.RandomAccounts,
		func(t testing.TB, ti simsx.TestInstance[*app.EthermintApp], _ []simtypes.Account) {
			a := ti.App
			t.Log("exporting genesis...\n")
			exported, err := a.ExportAppStateAndValidators(false, exportWithValidatorSet, exportAllModules)
			require.NoError(t, err)

			t.Log("importing genesis...\n")
			newTestInstance := simsx.NewSimulationAppInstance(t, ti.Cfg, NewSimApp)
			newApp := newTestInstance.App
			var genesisState map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(exported.AppState, &genesisState))
			ctxB := newApp.NewContextLegacy(true, cmtproto.Header{
				Height:  a.LastBlockHeight(),
				ChainID: config.ChainID,
			})
			_, err = newApp.ModuleManager.InitGenesis(ctxB, genesisState)
			if simapp.IsEmptyValidatorSetErr(err) {
				t.Skip("Skipping simulation as all validators have been unbonded")
				return
			}
			require.NoError(t, err)
			err = newApp.StoreConsensusParams(ctxB, exported.ConsensusParams)
			require.NoError(t, err)

			t.Log("comparing stores...")
			// skip certain prefixes
			skipPrefixes := map[string][][]byte{
				stakingtypes.StoreKey: {
					stakingtypes.UnbondingQueueKey, stakingtypes.RedelegationQueueKey, stakingtypes.ValidatorQueueKey,
				},
				authzkeeper.StoreKey:   {authzkeeper.GrantQueuePrefix},
				feegrant.StoreKey:      {feegrant.FeeAllowanceQueueKeyPrefix},
				slashingtypes.StoreKey: {slashingtypes.ValidatorMissedBlockBitmapKeyPrefix},
			}
			simapp.AssertEqualStores(t, a, newApp, a.SimulationManager().StoreDecoders, skipPrefixes)
		})
}

func TestAppSimulationAfterImport(t *testing.T) {
	return
	config := simcli.NewConfigFromFlags()
	config.ChainID = SimAppChainID
	config.BlockMaxGas = SimBlockMaxGas
	simsx.RunWithSeedAndRandAcc(
		t,
		config,
		NewSimApp,
		setupStateFactory,
		config.Seed,
		config.FuzzSeed,
		testutil.RandomAccounts,
		func(t testing.TB, ti simsx.TestInstance[*app.EthermintApp], accs []simtypes.Account) {
			a := ti.App
			t.Log("exporting genesis...\n")
			exported, err := a.ExportAppStateAndValidators(false, exportWithValidatorSet, exportAllModules)
			require.NoError(t, err)

			importGenesisStateFactory := func(a *app.EthermintApp) simsx.SimStateFactory {
				return simsx.SimStateFactory{
					Codec: a.AppCodec(),
					AppStateFn: func(r *rand.Rand, _ []simtypes.Account, config simtypes.Config) (json.RawMessage, []simtypes.Account, string, time.Time) {
						t.Log("importing genesis...\n")
						genesisTimestamp := time.Unix(config.GenesisTime, 0)

						_, err = a.InitChain(&abci.InitChainRequest{
							AppStateBytes: exported.AppState,
							ChainId:       config.ChainID,
							InitialHeight: exported.Height,
							Time:          genesisTimestamp,
						})
						if simapp.IsEmptyValidatorSetErr(err) {
							t.Skip("Skipping simulation as all validators have been unbonded")
							return nil, nil, "", time.Time{}
						}
						require.NoError(t, err)
						// use accounts from initial run
						return exported.AppState, accs, config.ChainID, genesisTimestamp
					},
					BlockedAddr:   a.BlockedAddresses(),
					AccountSource: a.AuthKeeper,
					BalanceSource: a.BankKeeper,
				}
			}
			ti.Cfg.InitialBlockHeight = int(exported.Height)
			simsx.RunWithSeedAndRandAcc(t, ti.Cfg, NewSimApp, importGenesisStateFactory, ti.Cfg.Seed, ti.Cfg.FuzzSeed, testutil.RandomAccounts)
		})
}

func TestAppStateDeterminism(t *testing.T) {
	const numTimesToRunPerSeed = 3
	var seeds []int64
	if s := simcli.NewConfigFromFlags().Seed; s != simcli.DefaultSeedValue {
		// We will be overriding the random seed and just run a single simulation on the provided seed value
		for j := 0; j < numTimesToRunPerSeed; j++ { // multiple rounds
			seeds = append(seeds, s)
		}
	} else {
		// setup with 3 random seeds
		for i := 0; i < 3; i++ {
			seed := rand.Int63()
			for j := 0; j < numTimesToRunPerSeed; j++ { // multiple rounds
				seeds = append(seeds, seed)
			}
		}
	}
	// overwrite default app config
	interBlockCachingAppFactory := func(logger log.Logger, db corestore.KVStoreWithBatch, traceStore io.Writer, loadLatest bool, appOpts servertypes.AppOptions, baseAppOptions ...func(*baseapp.BaseApp)) *app.EthermintApp {
		if FlagEnableStreamingValue {
			m := map[string]any{
				"streaming.abci.keys":             []string{"*"},
				"streaming.abci.plugin":           "abci_v1",
				"streaming.abci.stop-node-on-err": true,
			}
			others := appOpts
			appOpts = simsx.AppOptionsFn(func(k string) any {
				if v, ok := m[k]; ok {
					return v
				}
				return others.Get(k)
			})
		}
		return NewSimApp(logger, db, nil, true, appOpts, append(baseAppOptions, interBlockCacheOpt())...)
	}
	var mx sync.Mutex
	appHashResults := make(map[int64][][]byte)
	appSimLogger := make(map[int64][]simulation.LogWriter)
	captureAndCheckHash := func(t testing.TB, ti simsx.TestInstance[*app.EthermintApp], _ []simtypes.Account) {
		seed, appHash := ti.Cfg.Seed, ti.App.LastCommitID().Hash
		mx.Lock()
		otherHashes, execWriters := appHashResults[seed], appSimLogger[seed]
		if len(otherHashes) < numTimesToRunPerSeed-1 {
			appHashResults[seed], appSimLogger[seed] = append(otherHashes, appHash), append(execWriters, ti.ExecLogWriter)
		} else { // cleanup
			delete(appHashResults, seed)
			delete(appSimLogger, seed)
		}
		mx.Unlock()

		var failNow bool
		// and check that all app hashes per seed are equal for each iteration
		for i := 0; i < len(otherHashes); i++ {
			if !assert.Equal(t, otherHashes[i], appHash) {
				execWriters[i].PrintLogs()
				failNow = true
			}
		}
		if failNow {
			ti.ExecLogWriter.PrintLogs()
			t.Fatalf("non-determinism in seed %d", seed)
		}
	}
	// run simulations
	cfg := simcli.NewConfigFromFlags()
	cfg.ChainID = SimAppChainID
	for i := range seeds {
		seed := seeds[i]
		t.Run(fmt.Sprintf("seed: %d", seed), func(t *testing.T) {
			t.Parallel()
			simsx.RunWithSeedAndRandAcc(
				t,
				cfg,
				interBlockCachingAppFactory,
				setupStateFactory,
				seed,
				[]byte{},
				testutil.RandomAccounts,
				captureAndCheckHash,
			)
		})
	}

}
