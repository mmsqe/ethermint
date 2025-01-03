package ante_test

import (
	"context"
	"math"
	"math/big"
	"time"

	"cosmossdk.io/core/transaction"
	"cosmossdk.io/log"
	sdkmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	gogoprotoany "github.com/cosmos/gogoproto/types/any"
	"github.com/stretchr/testify/suite"
	protov2 "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"

	signingv1beta1 "cosmossdk.io/api/cosmos/tx/signing/v1beta1"
	banktypes "cosmossdk.io/x/bank/types"
	govv1 "cosmossdk.io/x/gov/types/v1"
	stakingtypes "cosmossdk.io/x/staking/types"
	"github.com/evmos/ethermint/app"
	cmdcfg "github.com/evmos/ethermint/cmd/config"
	"github.com/evmos/ethermint/ethereum/eip712"
	"github.com/evmos/ethermint/testutil"
	utiltx "github.com/evmos/ethermint/testutil/tx"
	ethermint "github.com/evmos/ethermint/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	authz "cosmossdk.io/x/authz"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	sdkante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/evmos/ethermint/crypto/ethsecp256k1"

	evtypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	govtypesv1 "cosmossdk.io/x/gov/types/v1"
	govtypes "cosmossdk.io/x/gov/types/v1beta1"
	cmtproto "github.com/cometbft/cometbft/api/cometbft/types/v1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	ante "github.com/evmos/ethermint/app/ante"
	"github.com/evmos/ethermint/tests"
	"github.com/evmos/ethermint/x/evm/statedb"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	feemarkettypes "github.com/evmos/ethermint/x/feemarket/types"
)

type AnteTestSuite struct {
	suite.Suite

	ctx                      sdk.Context
	app                      *app.EthermintApp
	clientCtx                client.Context
	anteHandler              sdk.AnteHandler
	priv                     cryptotypes.PrivKey
	ethSigner                ethtypes.Signer
	enableFeemarket          bool
	enableLondonHF           bool
	evmParamsOption          func(*evmtypes.Params)
	useLegacyEIP712Extension bool
	useLegacyEIP712TypedData bool
}

const TestGasLimit uint64 = 100000

func (suite *AnteTestSuite) StateDB() *statedb.StateDB {
	return statedb.New(suite.ctx, suite.app.EvmKeeper, statedb.NewEmptyTxConfig(common.BytesToHash(suite.ctx.HeaderHash())))
}

func (suite *AnteTestSuite) SetupTest() {
	cmdcfg.SetBech32Prefixes(sdk.GetConfig())
	checkTx := false
	priv, err := ethsecp256k1.GenerateKey()
	suite.Require().NoError(err)
	suite.priv = priv

	suite.app = testutil.Setup(checkTx, func(app *app.EthermintApp, genesis app.GenesisState) app.GenesisState {
		if suite.enableFeemarket {
			// setup feemarketGenesis params
			feemarketGenesis := feemarkettypes.DefaultGenesisState()
			feemarketGenesis.Params.EnableHeight = 1
			feemarketGenesis.Params.NoBaseFee = false
			// Verify feeMarket genesis
			err := feemarketGenesis.Validate()
			suite.Require().NoError(err)
			genesis[feemarkettypes.ModuleName] = app.AppCodec().MustMarshalJSON(feemarketGenesis)
		}
		evmGenesis := evmtypes.DefaultGenesisState()
		evmGenesis.Params.AllowUnprotectedTxs = false
		if !suite.enableLondonHF {
			maxInt := sdkmath.NewInt(math.MaxInt64)
			evmGenesis.Params.ChainConfig.LondonBlock = &maxInt
			evmGenesis.Params.ChainConfig.ArrowGlacierBlock = &maxInt
			evmGenesis.Params.ChainConfig.GrayGlacierBlock = &maxInt
			evmGenesis.Params.ChainConfig.MergeNetsplitBlock = &maxInt
			evmGenesis.Params.ChainConfig.ShanghaiTime = &maxInt
		}
		if suite.evmParamsOption != nil {
			suite.evmParamsOption(&evmGenesis.Params)
		}
		genesis[evmtypes.ModuleName] = app.AppCodec().MustMarshalJSON(evmGenesis)
		return genesis
	})
	header := cmtproto.Header{Height: 2, ChainID: testutil.TestnetChainID + "-1", Time: time.Now().UTC()}
	suite.ctx = suite.app.BaseApp.NewUncachedContext(checkTx, header).
		WithConsensusParams(*testutil.DefaultConsensusParams).
		WithMinGasPrices(sdk.NewDecCoins(sdk.NewDecCoin(evmtypes.DefaultEVMDenom, sdkmath.OneInt()))).
		WithBlockGasMeter(storetypes.NewGasMeter(1000000000000000000))
	suite.app.EvmKeeper.WithChainID(suite.ctx)

	infCtx := suite.ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	suite.app.AuthKeeper.Params.Set(infCtx, authtypes.DefaultParams())

	addr := sdk.AccAddress(priv.PubKey().Address().Bytes())
	acc := suite.app.AuthKeeper.NewAccountWithAddress(suite.ctx, addr)
	suite.app.AuthKeeper.SetAccount(suite.ctx, acc)

	encodingConfig := suite.app.EncodingConfig()
	// We're using TestMsg amino encoding in some tests, so register it here.
	encodingConfig.Amino.RegisterConcrete(&testdata.TestMsg{}, "testdata.TestMsg")
	eip712.SetEncodingConfig(encodingConfig)

	suite.clientCtx = client.Context{}.WithTxConfig(encodingConfig.TxConfig)

	anteHandler, err := ante.NewAnteHandler(ante.HandlerOptions{
		Environment:              runtime.NewEnvironment(nil, log.NewNopLogger(), runtime.EnvWithMsgRouterService(suite.app.MsgServiceRouter()), runtime.EnvWithQueryRouterService(suite.app.GRPCQueryRouter())), // nil is set as the kvstoreservice to avoid module access
		ConsensusKeeper:          suite.app.ConsensusParamsKeeper,
		AccountKeeper:            suite.app.AuthKeeper,
		AccountAbstractionKeeper: suite.app.AccountsKeeper,
		BankKeeper:               suite.app.BankKeeper,
		FeeMarketKeeper:          suite.app.FeeMarketKeeper,
		EvmKeeper:                suite.app.EvmKeeper,
		FeegrantKeeper:           suite.app.FeeGrantKeeper,
		SignModeHandler:          encodingConfig.TxConfig.SignModeHandler(),
		SigGasConsumer:           ante.DefaultSigVerificationGasConsumer,
		MaxTxGasWanted:           0,
		ExtensionOptionChecker:   ethermint.HasDynamicFeeExtensionOption,
		DisabledAuthzMsgs: []string{
			sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
			sdk.MsgTypeURL(&vestingtypes.BaseVestingAccount{}),
			sdk.MsgTypeURL(&vestingtypes.PermanentLockedAccount{}),
			sdk.MsgTypeURL(&vestingtypes.PeriodicVestingAccount{}),
		},
		UnorderedTxManager: suite.app.UnorderedTxManager,
	})
	suite.Require().NoError(err)

	suite.anteHandler = anteHandler
	suite.ethSigner = ethtypes.LatestSignerForChainID(suite.app.EvmKeeper.ChainID())

	// fund signer acc to pay for tx fees
	amt := sdkmath.NewInt(int64(math.Pow10(18) * 2))
	err = testutil.FundAccount(
		suite.app.BankKeeper,
		suite.ctx,
		suite.priv.PubKey().Address().Bytes(),
		sdk.NewCoins(sdk.NewCoin(testutil.BaseDenom, amt)),
	)
	suite.Require().NoError(err)

	suite.ctx = suite.ctx.WithBlockHeight(suite.ctx.BlockHeader().Height - 1)
	suite.ctx, err = testutil.Commit(suite.ctx, suite.app, time.Second*0, nil)
	suite.Require().NoError(err)
}

func (s *AnteTestSuite) BuildTestEthTx(
	from common.Address,
	to common.Address,
	amount *big.Int,
	input []byte,
	gasPrice *big.Int,
	gasFeeCap *big.Int,
	gasTipCap *big.Int,
	accesses *ethtypes.AccessList,
) *evmtypes.MsgEthereumTx {
	chainID := s.app.EvmKeeper.ChainID()
	nonce := s.app.EvmKeeper.GetNonce(
		s.ctx,
		common.BytesToAddress(from.Bytes()),
	)

	msgEthereumTx := evmtypes.NewTx(
		chainID,
		nonce,
		&to,
		amount,
		TestGasLimit,
		gasPrice,
		gasFeeCap,
		gasTipCap,
		input,
		accesses,
	)
	msgEthereumTx.From = from.Bytes()
	return msgEthereumTx
}

// CreateTestTx is a helper function to create a tx given multiple inputs.
func (suite *AnteTestSuite) CreateTestTx(
	msg *evmtypes.MsgEthereumTx, priv cryptotypes.PrivKey, accNum uint64, signCosmosTx bool,
	unsetExtensionOptions ...bool,
) authsigning.Tx {
	return suite.CreateTestTxBuilder(msg, priv, accNum, signCosmosTx).GetTx()
}

// CreateTestTxBuilder is a helper function to create a tx builder given multiple inputs.
func (suite *AnteTestSuite) CreateTestTxBuilder(
	msg *evmtypes.MsgEthereumTx, priv cryptotypes.PrivKey, accNum uint64, signCosmosTx bool,
	unsetExtensionOptions ...bool,
) client.TxBuilder {
	var option *gogoprotoany.Any
	var err error
	if len(unsetExtensionOptions) == 0 {
		option, err = codectypes.NewAnyWithValue(&evmtypes.ExtensionOptionsEthereumTx{})
		suite.Require().NoError(err)
	}

	txBuilder := suite.clientCtx.TxConfig.NewTxBuilder()
	builder, ok := txBuilder.(authtx.ExtensionOptionsTxBuilder)
	suite.Require().True(ok)

	if len(unsetExtensionOptions) == 0 {
		builder.SetExtensionOptions(option)
	}

	err = msg.Sign(suite.ethSigner, tests.NewSigner(priv))
	suite.Require().NoError(err)

	err = builder.SetMsgs(msg)
	suite.Require().NoError(err)

	txData := msg.AsTransaction()
	suite.Require().NotNil(txData)

	fees := sdk.NewCoins(sdk.NewCoin(evmtypes.DefaultEVMDenom, sdkmath.NewIntFromBigInt(msg.GetFee())))
	builder.SetFeeAmount(fees)
	builder.SetGasLimit(msg.GetGas())

	if signCosmosTx {
		// First round: we gather all the signer infos. We use the "set empty
		// signature" hack to do that.
		defaultSignMode, err := authsigning.APISignModeToInternal(suite.clientCtx.TxConfig.SignModeHandler().DefaultMode())
		suite.Require().NoError(err)

		sigV2 := signing.SignatureV2{
			PubKey: priv.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode:  defaultSignMode,
				Signature: nil,
			},
			Sequence: txData.Nonce(),
		}

		sigsV2 := []signing.SignatureV2{sigV2}

		err = txBuilder.SetSignatures(sigsV2...)
		suite.Require().NoError(err)

		// Second round: all signer infos are set, so each signer can sign.

		signerData := authsigning.SignerData{
			ChainID:       suite.ctx.ChainID(),
			AccountNumber: accNum,
			Sequence:      txData.Nonce(),
		}
		sigV2, err = tx.SignWithPrivKey(
			suite.ctx,
			defaultSignMode, signerData,
			txBuilder, priv, suite.clientCtx.TxConfig, txData.Nonce(),
		)
		suite.Require().NoError(err)

		sigsV2 = []signing.SignatureV2{sigV2}

		err = txBuilder.SetSignatures(sigsV2...)
		suite.Require().NoError(err)
	}

	return txBuilder
}

func (suite *AnteTestSuite) CreateTestCosmosTxBuilder(gasPrice sdkmath.Int, denom string, msgs ...sdk.Msg) client.TxBuilder {
	txBuilder := suite.clientCtx.TxConfig.NewTxBuilder()

	txBuilder.SetGasLimit(TestGasLimit)
	fees := &sdk.Coins{{Denom: denom, Amount: gasPrice.MulRaw(int64(TestGasLimit))}}
	txBuilder.SetFeeAmount(*fees)
	err := txBuilder.SetMsgs(msgs...)
	suite.Require().NoError(err)
	return txBuilder
}

func (suite *AnteTestSuite) CreateTestEIP712TxBuilderMsgSend(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	// Build MsgSend
	recipient := sdk.AccAddress(common.Address{}.Bytes())
	msgSend := banktypes.NewMsgSend(from.String(), recipient.String(), sdk.NewCoins(sdk.NewCoin(evmtypes.DefaultEVMDenom, sdkmath.NewInt(1))))
	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, msgSend)
}

func (suite *AnteTestSuite) CreateTestEIP712TxBuilderMsgDelegate(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	// Build MsgSend
	valEthAddr := tests.GenerateAddress()
	valAddr := sdk.ValAddress(valEthAddr.Bytes())
	msgSend := stakingtypes.NewMsgDelegate(from.String(), valAddr.String(), sdk.NewCoin(evmtypes.DefaultEVMDenom, sdkmath.NewInt(20)))
	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, msgSend)
}

func (suite *AnteTestSuite) CreateTestEIP712MsgCreateValidator(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	// Build MsgCreateValidator
	valAddr := sdk.ValAddress(from.Bytes())
	privEd := ed25519.GenPrivKey()
	msgCreate, err := stakingtypes.NewMsgCreateValidator(
		valAddr.String(),
		privEd.PubKey(),
		sdk.NewCoin(evmtypes.DefaultEVMDenom, sdkmath.NewInt(20)),
		stakingtypes.NewDescription("moniker", "indentity", "website", "security_contract", "details", nil),
		stakingtypes.NewCommissionRates(sdkmath.LegacyOneDec(), sdkmath.LegacyOneDec(), sdkmath.LegacyOneDec()),
		sdkmath.OneInt(),
	)
	suite.Require().NoError(err)
	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, msgCreate)
}

func (suite *AnteTestSuite) CreateTestEIP712MsgCreateValidator2(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	// Build MsgCreateValidator
	valAddr := sdk.ValAddress(from.Bytes())
	privEd := ed25519.GenPrivKey()
	msgCreate, err := stakingtypes.NewMsgCreateValidator(
		valAddr.String(),
		privEd.PubKey(),
		sdk.NewCoin(evmtypes.DefaultEVMDenom, sdkmath.NewInt(20)),
		// Ensure optional fields can be left blank
		stakingtypes.NewDescription("moniker", "indentity", "", "", "", nil),
		stakingtypes.NewCommissionRates(sdkmath.LegacyOneDec(), sdkmath.LegacyOneDec(), sdkmath.LegacyOneDec()),
		sdkmath.OneInt(),
	)
	suite.Require().NoError(err)
	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, msgCreate)
}

func (suite *AnteTestSuite) CreateTestEIP712SubmitProposal(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins, deposit sdk.Coins) client.TxBuilder {
	proposal, ok := govtypes.ContentFromProposalType("My proposal", "My description", govtypes.ProposalTypeText)
	suite.Require().True(ok)
	msgSubmit, err := govtypes.NewMsgSubmitProposal(proposal, deposit, from.String())
	suite.Require().NoError(err)
	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, msgSubmit)
}

func (suite *AnteTestSuite) CreateTestEIP712GrantAllowance(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	spendLimit := sdk.NewCoins(sdk.NewInt64Coin(evmtypes.DefaultEVMDenom, 10))
	threeHours := time.Now().Add(3 * time.Hour)
	basic := &feegrant.BasicAllowance{
		SpendLimit: spendLimit,
		Expiration: &threeHours,
	}
	granted := tests.GenerateAddress()
	grantedAddr := suite.app.AuthKeeper.NewAccountWithAddress(suite.ctx, granted.Bytes())
	msgGrant, err := feegrant.NewMsgGrantAllowance(basic, from.String(), grantedAddr.GetAddress().String())
	suite.Require().NoError(err)
	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, msgGrant)
}

func (suite *AnteTestSuite) CreateTestEIP712MsgEditValidator(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	valAddr := sdk.ValAddress(from.Bytes())
	msgEdit := stakingtypes.NewMsgEditValidator(
		valAddr.String(),
		stakingtypes.NewDescription("moniker", "identity", "website", "security_contract", "details", nil),
		nil,
		nil,
	)
	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, msgEdit)
}

func (suite *AnteTestSuite) CreateTestEIP712MsgSubmitEvidence(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	pk := ed25519.GenPrivKey()
	msgEvidence, err := evtypes.NewMsgSubmitEvidence(from.String(), &evtypes.Equivocation{
		Height:           11,
		Time:             time.Now().UTC(),
		Power:            100,
		ConsensusAddress: pk.PubKey().Address().String(),
	})
	suite.Require().NoError(err)

	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, msgEvidence)
}

func (suite *AnteTestSuite) CreateTestEIP712MsgVoteV1(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	msgVote := govtypesv1.NewMsgVote(from.String(), 1, govtypesv1.VoteOption_VOTE_OPTION_YES, "")
	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, msgVote)
}

func (suite *AnteTestSuite) CreateTestEIP712SubmitProposalV1(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	// Build V1 proposal messages. Must all be same-type, since EIP-712
	// does not support arrays of variable type.
	authAcc := suite.app.GovKeeper.GetGovernanceAccount(suite.ctx)

	proposal1, ok := govtypes.ContentFromProposalType("My proposal 1", "My description 1", govtypes.ProposalTypeText)
	suite.Require().True(ok)
	content1, err := govtypesv1.NewLegacyContent(
		proposal1,
		sdk.MustBech32ifyAddressBytes(sdk.GetConfig().GetBech32AccountAddrPrefix(), authAcc.GetAddress().Bytes()),
	)
	suite.Require().NoError(err)

	proposal2, ok := govtypes.ContentFromProposalType("My proposal 2", "My description 2", govtypes.ProposalTypeText)
	suite.Require().True(ok)
	content2, err := govtypesv1.NewLegacyContent(
		proposal2,
		sdk.MustBech32ifyAddressBytes(sdk.GetConfig().GetBech32AccountAddrPrefix(), authAcc.GetAddress().Bytes()),
	)
	suite.Require().NoError(err)

	proposalMsgs := []sdk.Msg{
		content1,
		content2,
	}

	// Build V1 proposal
	msgProposal, err := govtypesv1.NewMsgSubmitProposal(
		proposalMsgs,
		sdk.NewCoins(sdk.NewCoin(evmtypes.DefaultEVMDenom, sdkmath.NewInt(100))),
		sdk.MustBech32ifyAddressBytes(sdk.GetConfig().GetBech32AccountAddrPrefix(), from.Bytes()),
		"Metadata", "title", "summary", govv1.ProposalType_PROPOSAL_TYPE_STANDARD,
	)

	suite.Require().NoError(err)

	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, msgProposal)
}

func (suite *AnteTestSuite) CreateTestEIP712MsgExec(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	recipient := sdk.AccAddress(common.Address{}.Bytes())
	msgSend := banktypes.NewMsgSend(from.String(), recipient.String(), sdk.NewCoins(sdk.NewCoin(evmtypes.DefaultEVMDenom, sdkmath.NewInt(1))))
	msgExec := authz.NewMsgExec(from.String(), []sdk.Msg{msgSend})
	return suite.CreateTestEIP712SingleMessageTxBuilder(priv, chainId, gas, gasAmount, &msgExec)
}

func (suite *AnteTestSuite) CreateTestEIP712MultipleMsgSend(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	recipient := sdk.AccAddress(common.Address{}.Bytes())
	msgSend := banktypes.NewMsgSend(from.String(), recipient.String(), sdk.NewCoins(sdk.NewCoin(evmtypes.DefaultEVMDenom, sdkmath.NewInt(1))))
	return suite.CreateTestEIP712CosmosTxBuilder(priv, chainId, gas, gasAmount, []sdk.Msg{msgSend, msgSend, msgSend})
}

// Fails
func (suite *AnteTestSuite) CreateTestEIP712MultipleSignerMsgs(from sdk.AccAddress, priv cryptotypes.PrivKey, chainId string, gas uint64, gasAmount sdk.Coins) client.TxBuilder {
	recipient := sdk.AccAddress(common.Address{}.Bytes())
	msgSend1 := banktypes.NewMsgSend(from.String(), recipient.String(), sdk.NewCoins(sdk.NewCoin(evmtypes.DefaultEVMDenom, sdkmath.NewInt(1))))
	msgSend2 := banktypes.NewMsgSend(recipient.String(), from.String(), sdk.NewCoins(sdk.NewCoin(evmtypes.DefaultEVMDenom, sdkmath.NewInt(1))))
	return suite.CreateTestEIP712CosmosTxBuilder(priv, chainId, gas, gasAmount, []sdk.Msg{msgSend1, msgSend2})
}

func (suite *AnteTestSuite) CreateTestEIP712SingleMessageTxBuilder(
	priv cryptotypes.PrivKey, chainID string, gas uint64, gasAmount sdk.Coins, msg sdk.Msg,
) client.TxBuilder {
	return suite.CreateTestEIP712CosmosTxBuilder(priv, chainID, gas, gasAmount, []sdk.Msg{msg})
}

func (suite *AnteTestSuite) CreateTestEIP712CosmosTxBuilder(
	priv cryptotypes.PrivKey, chainID string, gas uint64, gasAmount sdk.Coins, msgs []sdk.Msg,
) client.TxBuilder {
	config := suite.clientCtx.TxConfig
	cosmosTxArgs := utiltx.CosmosTxArgs{
		TxCfg:   config,
		Priv:    priv,
		ChainID: chainID,
		Gas:     gas,
		Fees:    gasAmount,
		Msgs:    msgs,
	}

	builder, err := utiltx.PrepareEIP712CosmosTx(
		suite.ctx,
		suite.app,
		utiltx.EIP712TxArgs{
			CosmosTxArgs:       cosmosTxArgs,
			UseLegacyExtension: suite.useLegacyEIP712Extension,
			UseLegacyTypedData: suite.useLegacyEIP712TypedData,
		},
	)

	suite.Require().NoError(err)
	return builder
}

// Generate a set of pub/priv keys to be used in creating multi-keys
func (suite *AnteTestSuite) GenerateMultipleKeys(n int) ([]cryptotypes.PrivKey, []cryptotypes.PubKey) {
	privKeys := make([]cryptotypes.PrivKey, n)
	pubKeys := make([]cryptotypes.PubKey, n)
	for i := 0; i < n; i++ {
		privKey, err := ethsecp256k1.GenerateKey()
		suite.Require().NoError(err)
		privKeys[i] = privKey
		pubKeys[i] = privKey.PubKey()
	}
	return privKeys, pubKeys
}

// generateSingleSignature signs the given sign doc bytes using the given signType (EIP-712 or Standard)
func (suite *AnteTestSuite) generateSingleSignature(signMode signing.SignMode, privKey cryptotypes.PrivKey, signDocBytes []byte, signType string) (signature signing.SignatureV2) {
	var (
		msg []byte
		err error
	)

	msg = signDocBytes

	if signType == "EIP-712" {
		msg, err = eip712.GetEIP712BytesForMsg(signDocBytes)
		suite.Require().NoError(err)
	}

	sigBytes, _ := privKey.Sign(msg)
	sigData := &signing.SingleSignatureData{
		SignMode:  signMode,
		Signature: sigBytes,
	}

	return signing.SignatureV2{
		PubKey: privKey.PubKey(),
		Data:   sigData,
	}
}

// generateMultikeySignatures signs a set of messages using each private key within a given multi-key
func (suite *AnteTestSuite) generateMultikeySignatures(signMode signing.SignMode, privKeys []cryptotypes.PrivKey, signDocBytes []byte, signType string) (signatures []signing.SignatureV2) {
	n := len(privKeys)
	signatures = make([]signing.SignatureV2, n)

	for i := 0; i < n; i++ {
		privKey := privKeys[i]
		currentType := signType

		// If mixed type, alternate signing type on each iteration
		if signType == "mixed" {
			if i%2 == 0 {
				currentType = "EIP-712"
			} else {
				currentType = "Standard"
			}
		}

		signatures[i] = suite.generateSingleSignature(
			signMode,
			privKey,
			signDocBytes,
			currentType,
		)
	}

	return signatures
}

// RegisterAccount creates an account with the keeper and populates the initial balance
func (suite *AnteTestSuite) RegisterAccount(pubKey cryptotypes.PubKey, balance *big.Int) {
	acc := suite.app.AuthKeeper.NewAccountWithAddress(suite.ctx, sdk.AccAddress(pubKey.Address()))
	suite.app.AuthKeeper.SetAccount(suite.ctx, acc)

	suite.app.EvmKeeper.SetBalance(suite.ctx, common.BytesToAddress(pubKey.Address()), balance, evmtypes.DefaultEVMDenom)
}

// createSignerBytes generates sign doc bytes using the given parameters
func (suite *AnteTestSuite) createSignerBytes(chainId string, signMode signing.SignMode, pubKey cryptotypes.PubKey, txBuilder client.TxBuilder) []byte {
	acc := sdkante.GetSignerAcc(suite.ctx, suite.app.AuthKeeper, sdk.AccAddress(pubKey.Address()))
	suite.Require().NotNil(acc)
	anyPk, err := codectypes.NewAnyWithValue(pubKey)
	suite.Require().NoError(err)
	signerInfo := txsigning.SignerData{
		Address:       sdk.MustBech32ifyAddressBytes(sdk.GetConfig().GetBech32AccountAddrPrefix(), acc.GetAddress().Bytes()),
		ChainID:       chainId,
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
		PubKey: &anypb.Any{
			TypeUrl: anyPk.TypeUrl,
			Value:   anyPk.Value,
		},
	}
	builtTx := txBuilder.GetTx()
	adaptableTx, ok := builtTx.(authsigning.V2AdaptableTx)
	suite.Require().True(ok)
	txData := adaptableTx.GetSigningTxData()
	signerBytes, err := suite.clientCtx.TxConfig.SignModeHandler().GetSignBytes(
		context.Background(),
		signingv1beta1.SignMode(signMode),
		signerInfo,
		txData,
	)
	suite.Require().NoError(err)

	return signerBytes
}

// createBaseTxBuilder creates a TxBuilder to be used for Single- or Multi-signing
func (suite *AnteTestSuite) createBaseTxBuilder(msg sdk.Msg, gas uint64) client.TxBuilder {
	txBuilder := suite.clientCtx.TxConfig.NewTxBuilder()

	txBuilder.SetGasLimit(gas)
	txBuilder.SetFeeAmount(sdk.NewCoins(
		sdk.NewCoin("aphoton", sdkmath.NewInt(10000)),
	))

	err := txBuilder.SetMsgs(msg)
	suite.Require().NoError(err)

	txBuilder.SetMemo("")

	return txBuilder
}

// CreateTestSignedMultisigTx creates and sign a multi-signed tx for the given message. `signType` indicates whether to use standard signing ("Standard"),
// EIP-712 signing ("EIP-712"), or a mix of the two ("mixed").
func (suite *AnteTestSuite) CreateTestSignedMultisigTx(privKeys []cryptotypes.PrivKey, signMode signing.SignMode, msg sdk.Msg, chainId string, gas uint64, signType string) client.TxBuilder {
	pubKeys := make([]cryptotypes.PubKey, len(privKeys))
	for i, privKey := range privKeys {
		pubKeys[i] = privKey.PubKey()
	}

	// Re-derive multikey
	numKeys := len(privKeys)
	multiKey := kmultisig.NewLegacyAminoPubKey(numKeys, pubKeys)

	suite.RegisterAccount(multiKey, big.NewInt(10000000000))

	txBuilder := suite.createBaseTxBuilder(msg, gas)

	// Prepare signature field
	sig := multisig.NewMultisig(len(pubKeys))
	txBuilder.SetSignatures(signing.SignatureV2{
		PubKey: multiKey,
		Data:   sig,
	})

	signerBytes := suite.createSignerBytes(chainId, signMode, multiKey, txBuilder)

	// Sign for each key and update signature field
	sigs := suite.generateMultikeySignatures(signMode, privKeys, signerBytes, signType)
	for _, pkSig := range sigs {
		err := multisig.AddSignatureV2(sig, pkSig, pubKeys)
		suite.Require().NoError(err)
	}

	txBuilder.SetSignatures(signing.SignatureV2{
		PubKey: multiKey,
		Data:   sig,
	})

	return txBuilder
}

func (suite *AnteTestSuite) CreateTestSingleSignedTx(privKey cryptotypes.PrivKey, signMode signing.SignMode, msg sdk.Msg, chainId string, gas uint64, signType string) client.TxBuilder {
	pubKey := privKey.PubKey()

	suite.RegisterAccount(pubKey, big.NewInt(10000000000))

	txBuilder := suite.createBaseTxBuilder(msg, gas)

	// Prepare signature field
	sig := signing.SingleSignatureData{}
	txBuilder.SetSignatures(signing.SignatureV2{
		PubKey: pubKey,
		Data:   &sig,
	})

	signerBytes := suite.createSignerBytes(chainId, signMode, pubKey, txBuilder)

	sigData := suite.generateSingleSignature(signMode, privKey, signerBytes, signType)
	txBuilder.SetSignatures(sigData)

	return txBuilder
}

func NextFn(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

var _ sdk.Tx = &invalidTx{}

type invalidTx struct{}

func (invalidTx) Hash() [32]byte { return [32]byte{} }

func (invalidTx) GetSenders() ([]transaction.Identity, error) { return nil, nil }

func (invalidTx) GetReflectMessages() ([]protoreflect.Message, error) { return nil, nil }

func (invalidTx) GetMessages() ([]sdk.Msg, error) { return []sdk.Msg{nil}, nil }

func (invalidTx) GetGasLimit() (uint64, error) { return 0, nil }

func (invalidTx) Bytes() []byte { return nil }

func (invalidTx) GetMsgs() []sdk.Msg { return []sdk.Msg{nil} }

func (invalidTx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

func (invalidTx) ValidateBasic() error { return nil }
