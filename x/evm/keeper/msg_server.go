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
package keeper

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/core/event"
	govtypes "cosmossdk.io/x/gov/types"

	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	cmttypes "github.com/cometbft/cometbft/types"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/hashicorp/go-metrics"

	ethermint "github.com/evmos/ethermint/types"
	"github.com/evmos/ethermint/x/evm/types"
)

var _ types.MsgServer = &Keeper{}

// EthereumTx implements the gRPC MsgServer interface. It receives a transaction which is then
// executed (i.e applied) against the go-ethereum EVM. The provided SDK Context is set to the Keeper
// so that it can implements and call the StateDB methods without receiving it as a function
// parameter.
func (k *Keeper) EthereumTx(goCtx context.Context, msg *types.MsgEthereumTx) (*types.MsgEthereumTxResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	tx := msg.AsTransaction()

	labels := []metrics.Label{
		telemetry.NewLabel("tx_type", fmt.Sprintf("%d", tx.Type())),
	}
	if tx.To() == nil {
		labels = append(labels, telemetry.NewLabel("execution", "create"))
	} else {
		labels = append(labels, telemetry.NewLabel("execution", "call"))
	}

	response, err := k.ApplyTransaction(ctx, msg)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to apply transaction")
	}

	defer func() {
		telemetry.IncrCounterWithLabels(
			[]string{"tx", "msg", "ethereum_tx", "total"},
			1,
			labels,
		)

		if response.GasUsed != 0 {
			telemetry.IncrCounterWithLabels(
				[]string{"tx", "msg", "ethereum_tx", "gas_used", "total"},
				float32(response.GasUsed),
				labels,
			)

			// Observe which users define a gas limit >> gas used. Note, that
			// gas_limit and gas_used are always > 0
			gasLimit, err := ethermint.SafeInt64(tx.Gas())
			if err != nil {
				k.Logger.Error("failed to cast gas to int64", "error", err)
			}
			gasUsed, err := ethermint.SafeInt64(response.GasUsed)
			if err != nil {
				k.Logger.Error("failed to cast gasUsed to int64", "error", err)
			}
			gasRatio, err := sdkmath.LegacyNewDec(gasLimit).QuoInt64(gasUsed).Float64()
			if err == nil {
				telemetry.SetGaugeWithLabels(
					[]string{"tx", "msg", "ethereum_tx", "gas_limit", "per", "gas_used"},
					float32(gasRatio),
					labels,
				)
			}
		}
	}()

	attrs := []event.Attribute{
		event.NewAttribute(sdk.AttributeKeyAmount, tx.Value().String()),
		// add event for ethereum transaction hash format
		event.NewAttribute(types.AttributeKeyEthereumTxHash, response.Hash),
		// add event for eth tx gas used, we can't get it from cosmos tx result when it contains multiple eth tx msgs.
		event.NewAttribute(types.AttributeKeyTxGasUsed, strconv.FormatUint(response.GasUsed, 10)),
	}

	if len(ctx.TxBytes()) > 0 {
		// add event for tendermint transaction hash format
		hash := cmtbytes.HexBytes(cmttypes.Tx(ctx.TxBytes()).Hash())
		attrs = append(attrs, event.NewAttribute(types.AttributeKeyTxHash, hash.String()))
	}

	if to := tx.To(); to != nil {
		attrs = append(attrs, event.NewAttribute(types.AttributeKeyRecipient, types.HexAddress(to.Bytes())))
	}

	if response.Failed() {
		attrs = append(attrs, event.NewAttribute(types.AttributeKeyEthereumTxFailed, response.VmError))
	}

	// emit events
	k.EventService.EventManager(ctx).EmitKV(
		types.EventTypeEthereumTx,
		attrs...,
	)
	k.EventService.EventManager(ctx).EmitKV(
		sdk.EventTypeMessage,
		event.NewAttribute(sdk.AttributeKeyModule, types.AttributeValueCategory),
		event.NewAttribute(sdk.AttributeKeySender, types.HexAddress(msg.From)),
		event.NewAttribute(types.AttributeKeyTxType, strconv.Itoa(int(tx.Type()))),
	)

	return response, nil
}

// UpdateParams implements the gRPC MsgServer interface. When an UpdateParams
// proposal passes, it updates the module parameters. The update can only be
// performed if the requested authority is the Cosmos SDK governance module
// account.
func (k *Keeper) UpdateParams(goCtx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if k.authority.String() != req.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority, expected %s, got %s", k.authority.String(), req.Authority)
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := k.SetParams(ctx, req.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
