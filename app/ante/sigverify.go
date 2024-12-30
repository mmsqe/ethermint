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
	"errors"

	errorsmod "cosmossdk.io/errors"
	txsigning "cosmossdk.io/x/tx/signing"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	secp256k1dcrd "github.com/decred/dcrd/dcrec/secp256k1/v4"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/evmos/ethermint/crypto/ethsecp256k1"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
)

// VerifyEthSig validates checks that the registered chain id is the same as the one on the message, and
// that the signer address matches the one defined on the message.
// It's not skipped for RecheckTx, because it set `From` address which is critical from other ante handler to work.
// Failure in RecheckTx will prevent tx to be included into block, especially when CheckTx succeed, in which case user
// won't see the error message.
func VerifyEthSig(tx sdk.Tx, signer ethtypes.Signer) error {
	for _, msg := range tx.GetMsgs() {
		msgEthTx, ok := msg.(*evmtypes.MsgEthereumTx)
		if !ok {
			return errorsmod.Wrapf(errortypes.ErrUnknownRequest, "invalid message type %T, expected %T", msg, (*evmtypes.MsgEthereumTx)(nil))
		}

		if err := msgEthTx.VerifySender(signer); err != nil {
			return errorsmod.Wrapf(errortypes.ErrorInvalidSigner, "signature verification failed: %s", err.Error())
		}
	}

	return nil
}

type SigVerificationDecorator struct {
	authante.SigVerificationDecorator
}

func NewSigVerificationDecorator(
	ak authante.AccountKeeper,
	signModeHandler *txsigning.HandlerMap,
	sigGasConsumer authante.SignatureVerificationGasConsumer,
	aaKeeper authante.AccountAbstractionKeeper,
) SigVerificationDecorator {
	return SigVerificationDecorator{
		SigVerificationDecorator: authante.NewSigVerificationDecoratorWithVerifyOnCurve(
			ak, signModeHandler, sigGasConsumer, aaKeeper,
			func(pubKey cryptotypes.PubKey) (bool, error) {
				if pubKey.Bytes() != nil {
					if typedPubKey, ok := pubKey.(*ethsecp256k1.PubKey); ok {
						pubKeyObject, err := secp256k1dcrd.ParsePubKey(typedPubKey.Bytes())
						if err != nil {
							if errors.Is(err, secp256k1dcrd.ErrPubKeyNotOnCurve) {
								return true, errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "secp256k1 key is not on curve")
							}
							return true, err
						}
						if !pubKeyObject.IsOnCurve() {
							return true, errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "secp256k1 key is not on curve")
						}
						return true, nil
					}
				}
				return false, nil
			},
		),
	}
}
