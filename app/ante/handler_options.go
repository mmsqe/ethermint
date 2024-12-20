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
package ante

import (
	"context"

	"cosmossdk.io/core/appmodule"
	errorsmod "cosmossdk.io/errors"
	txsigning "cosmossdk.io/x/tx/signing"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/cosmos/cosmos-sdk/x/auth/ante/unorderedtx"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	evmtypes "github.com/evmos/ethermint/x/evm/types"
)

const EthSigVerificationResultCacheKey = "ante:EthSigVerificationResult"

// HandlerOptions extend the SDK's AnteHandler options by requiring the IBC
// channel keeper, EVM Keeper and Fee Market Keeper.
type HandlerOptions struct {
	Environment              appmodule.Environment
	ConsensusKeeper          ante.ConsensusKeeper
	AccountKeeper            evmtypes.AccountKeeper
	AccountAbstractionKeeper ante.AccountAbstractionKeeper
	BankKeeper               evmtypes.BankKeeper
	FeeMarketKeeper          FeeMarketKeeper
	EvmKeeper                EVMKeeper
	FeegrantKeeper           ante.FeegrantKeeper
	SignModeHandler          *txsigning.HandlerMap
	SigGasConsumer           ante.SignatureVerificationGasConsumer
	MaxTxGasWanted           uint64
	ExtensionOptionChecker   ante.ExtensionOptionChecker
	// use dynamic fee checker or the cosmos-sdk default one for native transactions
	DynamicFeeChecker bool
	DisabledAuthzMsgs []string
	ExtraDecorators   []sdk.AnteDecorator
	PendingTxListener PendingTxListener

	// see #494, just for benchmark, don't turn on on production
	UnsafeUnorderedTx bool

	UnorderedTxManager *unorderedtx.Manager
}

func (options HandlerOptions) validate() error {
	if options.AccountKeeper == nil {
		return errorsmod.Wrap(errortypes.ErrLogic, "account keeper is required for AnteHandler")
	}
	if options.BankKeeper == nil {
		return errorsmod.Wrap(errortypes.ErrLogic, "bank keeper is required for AnteHandler")
	}
	if options.SignModeHandler == nil {
		return errorsmod.Wrap(errortypes.ErrLogic, "sign mode handler is required for ante builder")
	}
	if options.FeeMarketKeeper == nil {
		return errorsmod.Wrap(errortypes.ErrLogic, "fee market keeper is required for AnteHandler")
	}
	if options.EvmKeeper == nil {
		return errorsmod.Wrap(errortypes.ErrLogic, "evm keeper is required for AnteHandler")
	}
	return nil
}

func newEthAnteHandler(options HandlerOptions) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		blockCfg, err := options.EvmKeeper.EVMBlockConfig(ctx, options.EvmKeeper.ChainID())
		if err != nil {
			return ctx, errorsmod.Wrap(errortypes.ErrLogic, err.Error())
		}
		evmParams := &blockCfg.Params
		evmDenom := evmParams.EvmDenom
		feemarketParams := &blockCfg.FeeMarketParams
		baseFee := blockCfg.BaseFee
		rules := blockCfg.Rules

		// all transactions must implement FeeTx
		_, ok := tx.(sdk.FeeTx)
		if !ok {
			return ctx, errorsmod.Wrapf(errortypes.ErrInvalidType, "invalid transaction type %T, expected sdk.FeeTx", tx)
		}

		// We need to setup an empty gas config so that the gas is consistent with Ethereum.
		ctx, err = SetupEthContext(ctx)
		if err != nil {
			return ctx, err
		}

		if err := CheckEthMempoolFee(ctx, tx, simulate, baseFee, evmDenom); err != nil {
			return ctx, err
		}

		if err := CheckEthMinGasPrice(tx, feemarketParams.MinGasPrice, baseFee); err != nil {
			return ctx, err
		}

		if err := ValidateEthBasic(ctx, tx, evmParams, baseFee); err != nil {
			return ctx, err
		}

		if v, ok := ctx.GetIncarnationCache(EthSigVerificationResultCacheKey); ok {
			if v != nil {
				err = v.(error)
			}
		} else {
			ethSigner := ethtypes.MakeSigner(blockCfg.ChainConfig, blockCfg.BlockNumber)
			err = VerifyEthSig(tx, ethSigner)
			ctx.SetIncarnationCache(EthSigVerificationResultCacheKey, err)
		}
		if err != nil {
			return ctx, err
		}

		// AccountGetter cache the account objects during the ante handler execution,
		// it's safe because there's no store branching in the ante handlers.
		accountGetter := NewCachedAccountGetter(ctx, options.AccountKeeper)

		if err := VerifyEthAccount(ctx, tx, options.EvmKeeper, evmDenom, accountGetter); err != nil {
			return ctx, err
		}

		if err := CheckEthCanTransfer(ctx, tx, baseFee, rules, options.EvmKeeper, evmParams); err != nil {
			return ctx, err
		}

		ctx, err = CheckEthGasConsume(
			ctx, tx, rules, options.EvmKeeper,
			baseFee, options.MaxTxGasWanted, evmDenom,
		)
		if err != nil {
			return ctx, err
		}

		if err := CheckAndSetEthSenderNonce(ctx, tx, options.AccountKeeper, options.UnsafeUnorderedTx, accountGetter); err != nil {
			return ctx, err
		}

		extraDecorators := options.ExtraDecorators
		if options.PendingTxListener != nil {
			extraDecorators = append(extraDecorators, newTxListenerDecorator(options.PendingTxListener))
		}
		if len(extraDecorators) > 0 {
			return sdk.ChainAnteDecorators(extraDecorators...)(ctx, tx, simulate)
		}
		return ctx, nil
	}
}

func newCosmosAnteHandler(ctx context.Context, options HandlerOptions, extra ...sdk.AnteDecorator) sdk.AnteHandler {
	evmParams := options.EvmKeeper.GetParams(ctx)
	feemarketParams := options.FeeMarketKeeper.GetParams(ctx)
	evmDenom := evmParams.EvmDenom
	chainID := options.EvmKeeper.ChainID()
	chainCfg := evmParams.GetChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	var txFeeChecker ante.TxFeeChecker
	if options.DynamicFeeChecker {
		txFeeChecker = NewDynamicFeeChecker(ethCfg, &evmParams, &feemarketParams)
	}
	decorators := []sdk.AnteDecorator{
		RejectMessagesDecorator{}, // reject MsgEthereumTxs
		// disable the Msg types that cannot be included on an authz.MsgExec msgs field
		NewAuthzLimiterDecorator(options.DisabledAuthzMsgs),
		ante.NewSetUpContextDecorator(options.Environment, options.ConsensusKeeper),
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(options.Environment),
		ante.NewTxTimeoutHeightDecorator(options.Environment),
		NewMinGasPriceDecorator(options.FeeMarketKeeper, evmDenom, &feemarketParams),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, txFeeChecker),
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler, options.SigGasConsumer, options.AccountAbstractionKeeper),
	}
	decorators = append(decorators, extra...)
	return sdk.ChainAnteDecorators(decorators...)
}
