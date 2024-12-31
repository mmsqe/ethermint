package ante_test

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/evmos/ethermint/app/ante"
	"github.com/evmos/ethermint/tests"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
)

func (suite *AnteTestSuite) TestEthSigVerificationDecorator() {
	addr, privKey := tests.NewAddrKey()

	signedTx := evmtypes.NewTxContract(suite.app.EvmKeeper.ChainID(), 1, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil)
	signedTx.From = addr.Bytes()
	err := signedTx.Sign(suite.ethSigner, tests.NewSigner(privKey))
	suite.Require().NoError(err)

	unprotectedTx := evmtypes.NewTxContract(nil, 1, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil)
	unprotectedTx.From = addr.Bytes()
	err = unprotectedTx.Sign(ethtypes.HomesteadSigner{}, tests.NewSigner(privKey))
	suite.Require().NoError(err)

	testCases := []struct {
		name      string
		msgs      []sdk.Msg
		reCheckTx bool
		expPass   bool
	}{
		{"ReCheckTx", invalidTx{}.GetMsgs(), true, false},
		{"invalid transaction type", invalidTx{}.GetMsgs(), false, false},
		{
			"invalid sender",
			evmtypes.NewTx(suite.app.EvmKeeper.ChainID(), 1, &addr, big.NewInt(10), 1000, big.NewInt(1), nil, nil, nil, nil).GetMsgs(),
			false,
			false,
		},
		{"successful signature verification", signedTx.GetMsgs(), false, true},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			err := ante.VerifyEthSig(tc.msgs, suite.ethSigner)

			if tc.expPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
	suite.evmParamsOption = nil
}
