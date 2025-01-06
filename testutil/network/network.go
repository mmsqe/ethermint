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
package network

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	"github.com/cometbft/cometbft/node"
	tmrpcclient "github.com/cometbft/cometbft/rpc/client"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	pruningtypes "cosmossdk.io/store/pruning/types"
	banktypes "cosmossdk.io/x/bank/types"
	stakingtypes "cosmossdk.io/x/staking/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/server/api"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"

	"github.com/evmos/ethermint/crypto/hd"
	"github.com/evmos/ethermint/server/config"
	testutilconfig "github.com/evmos/ethermint/testutil/config"
	ethermint "github.com/evmos/ethermint/types"
	evmtypes "github.com/evmos/ethermint/x/evm/types"

	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/evmos/ethermint/app"
)

// network lock to only allow one test network at a time
var (
	lock     = new(sync.Mutex)
	portPool = make(chan string, 200)
)

// AppConstructor defines a function which accepts a network configuration and
// creates an ABCI Application to provide to Tendermint.
type AppConstructor = func(val Validator) servertypes.Application

// NewAppConstructor returns a new simapp AppConstructor
func NewAppConstructor(chainID string) AppConstructor {
	return func(val Validator) servertypes.Application {
		return app.NewEthermintApp(
			val.Ctx.Logger,
			dbm.NewMemDB(),
			nil,
			true,
			simtestutil.NewAppOptionsWithFlagHome(val.Ctx.Config.RootDir),
			baseapp.SetPruning(pruningtypes.NewPruningOptionsFromString(val.AppConfig.Pruning)),
			baseapp.SetMinGasPrices(val.AppConfig.MinGasPrices),
			baseapp.SetChainID(chainID),
		)
	}
}

// Config defines the necessary configuration used to bootstrap and start an
// in-process local testing network.
type Config struct {
	KeyringOptions    []keyring.Option // keyring configuration options
	Codec             codec.Codec
	LegacyAmino       *codec.LegacyAmino // TODO: Remove!
	InterfaceRegistry codectypes.InterfaceRegistry
	TxConfig          client.TxConfig
	AccountRetriever  client.AccountRetriever
	AppConstructor    AppConstructor   // the ABCI application constructor
	GenesisState      app.GenesisState // custom gensis state to provide
	TimeoutCommit     time.Duration    // the consensus commitment timeout
	AccountTokens     sdkmath.Int      // the amount of unique validator tokens (e.g. 1000node0)
	StakingTokens     sdkmath.Int      // the amount of tokens each validator has available to stake
	BondedTokens      sdkmath.Int      // the amount of tokens each validator stakes
	NumValidators     int              // the total number of validators to create and bond
	ChainID           string           // the network chain-id
	BondDenom         string           // the staking bond denomination
	MinGasPrices      string           // the minimum gas prices each validator will accept
	PruningStrategy   string           // the pruning strategy each validator will have
	SigningAlgo       string           // signing algorithm for keys
	RPCAddress        string           // RPC listen address (including port)
	JSONRPCAddress    string           // JSON-RPC listen address (including port)
	APIAddress        string           // REST API listen address (including port)
	GRPCAddress       string           // GRPC server listen address (including port)
	EnableTMLogging   bool             // enable Tendermint logging to STDOUT
	CleanupDir        bool             // remove base temporary directory during cleanup
	PrintMnemonic     bool             // print the mnemonic of first validator as log output for testing
}

// DefaultConfig returns a sane default configuration suitable for nearly all
// testing requirements.
func DefaultConfig() Config {
	encCfg := testutilconfig.MakeConfigForTest()
	i, err := rand.Int(rand.Reader, new(big.Int).SetInt64(int64(9999999999999)))
	if err != nil {
		panic(err)
	}
	chainID := fmt.Sprintf("ethermint_%d-1", i.Int64()+1)
	return Config{
		Codec:             encCfg.Codec,
		TxConfig:          encCfg.TxConfig,
		LegacyAmino:       encCfg.Amino,
		InterfaceRegistry: encCfg.InterfaceRegistry,
		AccountRetriever:  authtypes.AccountRetriever{},
		AppConstructor:    NewAppConstructor(chainID),
		// GenesisState:      app.ModuleBasicsForTest.DefaultGenesis(encCfg.Codec), // TOFIX
		TimeoutCommit:   2 * time.Second,
		ChainID:         chainID,
		NumValidators:   4,
		BondDenom:       ethermint.AttoPhoton,
		MinGasPrices:    fmt.Sprintf("0.000006%s", ethermint.AttoPhoton),
		AccountTokens:   sdk.TokensFromConsensusPower(1000, ethermint.PowerReduction),
		StakingTokens:   sdk.TokensFromConsensusPower(500, ethermint.PowerReduction),
		BondedTokens:    sdk.TokensFromConsensusPower(100, ethermint.PowerReduction),
		PruningStrategy: pruningtypes.PruningOptionNothing,
		CleanupDir:      true,
		SigningAlgo:     string(hd.EthSecp256k1Type),
		KeyringOptions:  []keyring.Option{hd.EthSecp256k1Option()},
		PrintMnemonic:   false,
	}
}

type (
	// Network defines a local in-process testing network using SimApp. It can be
	// configured to start any number of validators, each with its own RPC and API
	// clients. Typically, this test network would be used in client and integration
	// testing where user input is expected.
	//
	// Note, due to Tendermint constraints in regards to RPC functionality, there
	// may only be one test network running at a time. Thus, any caller must be
	// sure to Cleanup after testing is finished in order to allow other tests
	// to create networks. In addition, only the first validator will have a valid
	// RPC and API server/client.
	Network struct {
		Logger     Logger
		BaseDir    string
		Validators []*Validator

		Config Config
	}

	// Validator defines an in-process Tendermint validator node. Through this object,
	// a client can make RPC and API calls and interact with any client command
	// or handler.
	Validator struct {
		AppConfig     *config.Config
		ClientCtx     client.Context
		Ctx           *server.Context
		Dir           string
		NodeID        string
		PubKey        cryptotypes.PubKey
		Moniker       string
		APIAddress    string
		RPCAddress    string
		P2PAddress    string
		Address       sdk.AccAddress
		ValAddress    sdk.ValAddress
		RPCClient     tmrpcclient.Client
		JSONRPCClient *ethclient.Client

		tmNode   *node.Node
		api      *api.Server
		grpc     *grpc.Server
		grpcWeb  *http.Server
		jsonrpc  *http.Server
		errGroup *errgroup.Group
		cancelFn context.CancelFunc
	}
)

// Logger is a network logger interface that exposes testnet-level Log() methods for an in-process testing network
// This is not to be confused with logging that may happen at an individual node or validator level
type Logger interface {
	Log(args ...interface{})
	Logf(format string, args ...interface{})
}

var (
	_ Logger = (*testing.T)(nil)
	_ Logger = (*CLILogger)(nil)
)

type CLILogger struct {
	cmd *cobra.Command
}

func (s CLILogger) Log(args ...interface{}) {
	s.cmd.Println(args...)
}

func (s CLILogger) Logf(format string, args ...interface{}) {
	s.cmd.Printf(format, args...)
}

func NewCLILogger(cmd *cobra.Command) CLILogger {
	return CLILogger{cmd}
}

// New creates a new Network for integration tests or in-process testnets run via the CLI
func New(l Logger, baseDir string, cfg Config, keyType string) (*Network, error) {
	// only one caller/test can create and use a network at a time
	l.Log("acquiring test network lock")
	lock.Lock()

	if !ethermint.IsValidChainID(cfg.ChainID) {
		return nil, fmt.Errorf("invalid chain-id: %s", cfg.ChainID)
	}

	network := &Network{
		Logger:     l,
		BaseDir:    baseDir,
		Validators: make([]*Validator, cfg.NumValidators),
		Config:     cfg,
	}

	l.Logf("preparing test network with chain-id \"%s\"\n", cfg.ChainID)

	monikers := make([]string, cfg.NumValidators)
	nodeIDs := make([]string, cfg.NumValidators)
	valPubKeys := make([]cryptotypes.PubKey, cfg.NumValidators)

	var (
		genAccounts []authtypes.GenesisAccount
		genBalances []banktypes.Balance
		genFiles    []string
	)

	buf := bufio.NewReader(os.Stdin)

	// generate private keys, node IDs, and initial transactions
	for i := 0; i < cfg.NumValidators; i++ {
		appCfg := config.DefaultConfig()
		appCfg.Pruning = cfg.PruningStrategy
		appCfg.MinGasPrices = cfg.MinGasPrices
		appCfg.API.Enable = true
		appCfg.API.Swagger = false
		appCfg.Telemetry.Enabled = false
		appCfg.Telemetry.GlobalLabels = [][]string{{"chain_id", cfg.ChainID}}

		ctx := server.NewDefaultContext()
		cmtCfg := ctx.Config
		cmtCfg.Consensus.TimeoutCommit = cfg.TimeoutCommit

		// Only allow the first validator to expose an RPC, API and gRPC
		// server/client due to Tendermint in-process constraints.
		apiAddr := ""
		cmtCfg.RPC.ListenAddress = ""
		appCfg.GRPC.Enable = false
		apiListenAddr := ""
		if i == 0 {
			if cfg.APIAddress != "" {
				apiListenAddr = cfg.APIAddress
			} else {
				if len(portPool) == 0 {
					return nil, fmt.Errorf("failed to get port for API server")
				}
				port := <-portPool
				apiListenAddr = fmt.Sprintf("tcp://0.0.0.0:%s", port)
			}

			appCfg.API.Address = apiListenAddr
			apiURL, err := url.Parse(apiListenAddr)
			if err != nil {
				return nil, err
			}
			apiAddr = fmt.Sprintf("http://%s:%s", apiURL.Hostname(), apiURL.Port())

			if cfg.RPCAddress != "" {
				cmtCfg.RPC.ListenAddress = cfg.RPCAddress
			} else {
				if len(portPool) == 0 {
					return nil, fmt.Errorf("failed to get port for RPC server")
				}
				port := <-portPool
				cmtCfg.RPC.ListenAddress = fmt.Sprintf("tcp://0.0.0.0:%s", port)
			}

			if cfg.GRPCAddress != "" {
				appCfg.GRPC.Address = cfg.GRPCAddress
			} else {
				if len(portPool) == 0 {
					return nil, fmt.Errorf("failed to get port for GRPC server")
				}
				port := <-portPool
				appCfg.GRPC.Address = fmt.Sprintf("0.0.0.0:%s", port)
			}
			appCfg.GRPC.Enable = true

			if cfg.JSONRPCAddress != "" {
				appCfg.JSONRPC.Address = cfg.JSONRPCAddress
			} else {
				if len(portPool) == 0 {
					return nil, fmt.Errorf("failed to get port for JSON-RPC server")
				}
				port := <-portPool
				appCfg.JSONRPC.Address = fmt.Sprintf("0.0.0.0:%s", port)
			}
			appCfg.JSONRPC.Enable = true
			appCfg.JSONRPC.API = config.GetAPINamespaces()
		}

		logger := log.NewNopLogger()
		if cfg.EnableTMLogging {
			logger = log.NewLogger(os.Stdout)
		}

		ctx.Logger = logger

		nodeDirName := fmt.Sprintf("node%d", i)
		nodeDir := filepath.Join(network.BaseDir, nodeDirName, "evmosd")
		clientDir := filepath.Join(network.BaseDir, nodeDirName, "evmoscli")
		gentxsDir := filepath.Join(network.BaseDir, "gentxs")

		err := os.MkdirAll(filepath.Join(nodeDir, "config"), 0o750)
		if err != nil {
			return nil, err
		}

		err = os.MkdirAll(clientDir, 0o750)
		if err != nil {
			return nil, err
		}

		cmtCfg.SetRoot(nodeDir)
		cmtCfg.Moniker = nodeDirName
		monikers[i] = nodeDirName

		if len(portPool) == 0 {
			return nil, fmt.Errorf("failed to get port for Proxy server")
		}
		port := <-portPool
		proxyAddr := fmt.Sprintf("tcp://0.0.0.0:%s", port)
		cmtCfg.ProxyApp = proxyAddr

		if len(portPool) == 0 {
			return nil, fmt.Errorf("failed to get port for Proxy server")
		}
		port = <-portPool
		p2pAddr := fmt.Sprintf("tcp://0.0.0.0:%s", port)
		cmtCfg.P2P.ListenAddress = p2pAddr
		cmtCfg.P2P.AddrBookStrict = false
		cmtCfg.P2P.AllowDuplicateIP = true

		nodeID, pubKey, err := genutil.InitializeNodeValidatorFiles(cmtCfg, keyType)
		if err != nil {
			return nil, err
		}
		nodeIDs[i] = nodeID
		valPubKeys[i] = pubKey

		kb, err := keyring.New(sdk.KeyringServiceName(), keyring.BackendTest, clientDir, buf, cfg.Codec, cfg.KeyringOptions...)
		if err != nil {
			return nil, err
		}

		keyringAlgos, _ := kb.SupportedAlgorithms()
		algo, err := keyring.NewSigningAlgoFromString(cfg.SigningAlgo, keyringAlgos)
		if err != nil {
			return nil, err
		}

		addr, secret, err := testutil.GenerateSaveCoinKey(kb, nodeDirName, "", true, algo, sdk.GetFullBIP44Path())
		if err != nil {
			return nil, err
		}

		// if PrintMnemonic is set to true, we print the first validator node's secret to the network's logger
		// for debugging and manual testing
		if cfg.PrintMnemonic && i == 0 {
			printMnemonic(l, secret)
		}

		info := map[string]string{"secret": secret}
		infoBz, err := json.Marshal(info)
		if err != nil {
			return nil, err
		}

		// save private key seed words
		err = WriteFile(fmt.Sprintf("%v.json", "key_seed"), clientDir, infoBz)
		if err != nil {
			return nil, err
		}

		balances := sdk.NewCoins(
			sdk.NewCoin(fmt.Sprintf("%stoken", nodeDirName), cfg.AccountTokens),
			sdk.NewCoin(cfg.BondDenom, cfg.StakingTokens),
		)

		genFiles = append(genFiles, cmtCfg.GenesisFile())
		genBalances = append(genBalances, banktypes.Balance{Address: addr.String(), Coins: balances.Sort()})
		genAccounts = append(genAccounts, &ethermint.EthAccount{
			BaseAccount: authtypes.NewBaseAccount(addr, nil, 0, 0),
			CodeHash:    common.BytesToHash(evmtypes.EmptyCodeHash).Hex(),
		})

		commission, err := sdkmath.LegacyNewDecFromStr("0.5")
		if err != nil {
			return nil, err
		}

		createValMsg, err := stakingtypes.NewMsgCreateValidator(
			addr.String(),
			valPubKeys[i],
			sdk.NewCoin(cfg.BondDenom, cfg.BondedTokens),
			stakingtypes.NewDescription(nodeDirName, "", "", "", "", nil),
			stakingtypes.NewCommissionRates(commission, sdkmath.LegacyOneDec(), sdkmath.LegacyOneDec()),
			sdkmath.OneInt(),
		)
		if err != nil {
			return nil, err
		}

		p2pURL, err := url.Parse(p2pAddr)
		if err != nil {
			return nil, err
		}

		memo := fmt.Sprintf("%s@%s:%s", nodeIDs[i], p2pURL.Hostname(), p2pURL.Port())
		fee := sdk.NewCoins(sdk.NewCoin(cfg.BondDenom, sdkmath.NewInt(0)))
		txBuilder := cfg.TxConfig.NewTxBuilder()
		err = txBuilder.SetMsgs(createValMsg)
		if err != nil {
			return nil, err
		}
		txBuilder.SetFeeAmount(fee)    // Arbitrary fee
		txBuilder.SetGasLimit(1000000) // Need at least 100386
		txBuilder.SetMemo(memo)

		txFactory := tx.Factory{}
		txFactory = txFactory.
			WithChainID(cfg.ChainID).
			WithMemo(memo).
			WithKeybase(kb).
			WithTxConfig(cfg.TxConfig)

		clientCtx := client.Context{}.
			WithKeyringDir(clientDir).
			WithKeyring(kb).
			WithHomeDir(cmtCfg.RootDir).
			WithChainID(cfg.ChainID).
			WithInterfaceRegistry(cfg.InterfaceRegistry).
			WithCodec(cfg.Codec).
			WithLegacyAmino(cfg.LegacyAmino).
			WithTxConfig(cfg.TxConfig).
			WithAccountRetriever(cfg.AccountRetriever)

		if err := tx.Sign(clientCtx, txFactory, nodeDirName, txBuilder, true); err != nil {
			return nil, err
		}

		txBz, err := cfg.TxConfig.TxJSONEncoder()(txBuilder.GetTx())
		if err != nil {
			return nil, err
		}

		if err := WriteFile(fmt.Sprintf("%v.json", nodeDirName), gentxsDir, txBz); err != nil {
			return nil, err
		}

		customAppTemplate, _ := config.AppConfig(ethermint.AttoPhoton)
		if err := srvconfig.SetConfigTemplate(customAppTemplate); err != nil {
			return nil, err
		}
		if err := srvconfig.WriteConfigFile(filepath.Join(nodeDir, "config/app.toml"), appCfg); err != nil {
			return nil, err
		}

		ctx.Viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
		ctx.Viper.SetConfigFile(filepath.Join(nodeDir, "config/app.toml"))
		err = ctx.Viper.ReadInConfig()
		if err != nil {
			return nil, err
		}

		network.Validators[i] = &Validator{
			AppConfig:  appCfg,
			ClientCtx:  clientCtx,
			Ctx:        ctx,
			Dir:        filepath.Join(network.BaseDir, nodeDirName),
			NodeID:     nodeID,
			PubKey:     pubKey,
			Moniker:    nodeDirName,
			RPCAddress: cmtCfg.RPC.ListenAddress,
			P2PAddress: cmtCfg.P2P.ListenAddress,
			APIAddress: apiAddr,
			Address:    addr,
			ValAddress: sdk.ValAddress(addr),
		}
	}

	err := initGenFiles(cfg, genAccounts, genBalances, genFiles)
	if err != nil {
		return nil, err
	}
	err = collectGenFiles(cfg, network.Validators, network.BaseDir)
	if err != nil {
		return nil, err
	}

	l.Log("starting test network...")
	for _, v := range network.Validators {
		err := startInProcess(cfg, v)
		if err != nil {
			return nil, err
		}
	}

	l.Log("started test network")

	// Ensure we cleanup incase any test was abruptly halted (e.g. SIGINT) as any
	// defer in a test would not be called.
	trapSignal(network.Cleanup)

	return network, nil
}

// LatestHeight returns the latest height of the network or an error if the
// query fails or no validators exist.
func (n *Network) LatestHeight() (int64, error) {
	if len(n.Validators) == 0 {
		return 0, errors.New("no validators available")
	}

	status, err := n.Validators[0].RPCClient.Status(context.Background())
	if err != nil {
		return 0, err
	}

	return status.SyncInfo.LatestBlockHeight, nil
}

// WaitForHeight performs a blocking check where it waits for a block to be
// committed after a given block. If that height is not reached within a timeout,
// an error is returned. Regardless, the latest height queried is returned.
func (n *Network) WaitForHeight(h int64) (int64, error) {
	return n.WaitForHeightWithTimeout(h, 10*time.Second)
}

// WaitForHeightWithTimeout is the same as WaitForHeight except the caller can
// provide a custom timeout.
func (n *Network) WaitForHeightWithTimeout(h int64, t time.Duration) (int64, error) {
	ticker := time.NewTicker(time.Second)
	timeout := time.After(t)

	if len(n.Validators) == 0 {
		return 0, errors.New("no validators available")
	}

	var latestHeight int64
	val := n.Validators[0]

	for {
		select {
		case <-timeout:
			ticker.Stop()
			return latestHeight, errors.New("timeout exceeded waiting for block")
		case <-ticker.C:
			status, err := val.RPCClient.Status(context.Background())
			if err == nil && status != nil {
				latestHeight = status.SyncInfo.LatestBlockHeight
				if latestHeight >= h {
					return latestHeight, nil
				}
			}
		}
	}
}

// WaitForNextBlock waits for the next block to be committed, returning an error
// upon failure.
func (n *Network) WaitForNextBlock() error {
	lastBlock, err := n.LatestHeight()
	if err != nil {
		return err
	}

	_, err = n.WaitForHeight(lastBlock + 1)
	if err != nil {
		return err
	}

	return err
}

// Cleanup removes the root testing (temporary) directory and stops both the
// Tendermint and API services. It allows other callers to create and start
// test networks. This method must be called when a test is finished, typically
// in a defer.
func (n *Network) Cleanup() {
	defer func() {
		lock.Unlock()
		n.Logger.Log("released test network lock")
	}()

	n.Logger.Log("cleaning up test network...")

	for _, v := range n.Validators {
		if v.tmNode != nil && v.tmNode.IsRunning() {
			_ = v.tmNode.Stop()
		}

		if v.api != nil {
			_ = v.api.Close()
		}

		if v.grpc != nil {
			v.grpc.Stop()
			if v.grpcWeb != nil {
				_ = v.grpcWeb.Close()
			}
		}

		if v.jsonrpc != nil {
			_ = v.jsonrpc.Close()
		}
	}

	if n.Config.CleanupDir {
		_ = os.RemoveAll(n.BaseDir)
	}

	n.Logger.Log("finished cleaning up test network")
}

// printMnemonic prints a provided mnemonic seed phrase on a network logger
// for debugging and manual testing
func printMnemonic(l Logger, secret string) {
	lines := []string{
		"THIS MNEMONIC IS FOR TESTING PURPOSES ONLY",
		"DO NOT USE IN PRODUCTION",
		"",
		strings.Join(strings.Fields(secret)[0:8], " "),
		strings.Join(strings.Fields(secret)[8:16], " "),
		strings.Join(strings.Fields(secret)[16:24], " "),
	}

	lineLengths := make([]int, len(lines))
	for i, line := range lines {
		lineLengths[i] = len(line)
	}

	maxLineLength := 0
	for _, lineLen := range lineLengths {
		if lineLen > maxLineLength {
			maxLineLength = lineLen
		}
	}

	l.Log("\n")
	l.Log(strings.Repeat("+", maxLineLength+8))
	for _, line := range lines {
		l.Logf("++  %s  ++\n", centerText(line, maxLineLength))
	}
	l.Log(strings.Repeat("+", maxLineLength+8))
	l.Log("\n")
}

// centerText centers text across a fixed width, filling either side with whitespace buffers
func centerText(text string, width int) string {
	textLen := len(text)
	leftBuffer := strings.Repeat(" ", (width-textLen)/2)
	rightBuffer := strings.Repeat(" ", (width-textLen)/2+(width-textLen)%2)

	return fmt.Sprintf("%s%s%s", leftBuffer, text, rightBuffer)
}

// trapSignal traps SIGINT and SIGTERM and calls os.Exit once a signal is received.
func trapSignal(cleanupFunc func()) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigs

		if cleanupFunc != nil {
			cleanupFunc()
		}
		exitCode := 128

		switch sig {
		case syscall.SIGINT:
			exitCode += int(syscall.SIGINT)
		case syscall.SIGTERM:
			exitCode += int(syscall.SIGTERM)
		}

		os.Exit(exitCode)
	}()
}
