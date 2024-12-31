package types_test

import (
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/evmos/ethermint/crypto/ethsecp256k1"
	"github.com/evmos/ethermint/encoding"
	"github.com/evmos/ethermint/tests"
	ethermint "github.com/evmos/ethermint/types"

	"github.com/evmos/ethermint/x/evm/types"
)

const invalidFromAddress = "0x0000"

type MsgsTestSuite struct {
	suite.Suite

	signer        keyring.Signer
	from          common.Address
	to            common.Address
	chainID       *big.Int
	hundredBigInt *big.Int

	clientCtx client.Context
}

func TestMsgsTestSuite(t *testing.T) {
	suite.Run(t, new(MsgsTestSuite))
}

func (suite *MsgsTestSuite) SetupTest() {
	from, privFrom := tests.NewAddrKey()

	suite.signer = tests.NewSigner(privFrom)
	suite.from = from
	suite.to = tests.GenerateAddress()
	suite.chainID = big.NewInt(1)
	suite.hundredBigInt = big.NewInt(100)
	suite.clientCtx = client.Context{}.WithTxConfig(encoding.MakeConfig().TxConfig)
}

func (suite *MsgsTestSuite) TestMsgEthereumTx_Constructor() {
	msg := types.NewTx(nil, 0, &suite.to, nil, 100000, nil, nil, nil, []byte("test"), nil)

	// suite.Require().Equal(msg.Data.To, suite.to.Hex())
	suite.Require().Equal(msg.Route(), types.RouterKey)
	suite.Require().Equal(msg.Type(), types.TypeMsgEthereumTx)
	// suite.Require().NotNil(msg.To())
	suite.Require().Equal(msg.GetMsgs(), []sdk.Msg{msg})
	suite.Require().Panics(func() { msg.GetSignBytes() })

	msg = types.NewTxContract(nil, 0, nil, 100000, nil, nil, nil, []byte("test"), nil)
	suite.Require().NotNil(msg)
	// suite.Require().Empty(msg.Data.To)
	// suite.Require().Nil(msg.To())
}

func (suite *MsgsTestSuite) TestMsgEthereumTx_BuildTx() {
	testCases := []struct {
		name     string
		msg      *types.MsgEthereumTx
		expError bool
	}{
		{
			"build tx - pass",
			types.NewTx(nil, 0, &suite.to, nil, 100000, big.NewInt(1), big.NewInt(1), big.NewInt(0), []byte("test"), nil),
			false,
		},
	}

	for _, tc := range testCases {
		if strings.Contains(tc.name, "nil data") {
			tc.msg.Data = nil
		}

		tx, err := tc.msg.BuildTx(suite.clientCtx.TxConfig.NewTxBuilder(), "aphoton")
		if tc.expError {
			suite.Require().Error(err)
		} else {
			suite.Require().NoError(err)

			suite.Require().Empty(tx.GetMemo())
			// mmsqe
			// suite.Require().Empty(tx.GetTimeoutHeight())
			suite.Require().Equal(uint64(100000), tx.GetGas())
			suite.Require().Equal(sdk.NewCoins(sdk.NewCoin("aphoton", sdkmath.NewInt(100000))), tx.GetFee())
		}
	}
}

func (suite *MsgsTestSuite) TestMsgEthereumTx_ValidateBasic() {
	hundredInt := big.NewInt(100)
	zeroInt := big.NewInt(0)
	minusOneInt := big.NewInt(-1)
	exp_2_255 := new(big.Int).Exp(big.NewInt(2), big.NewInt(255), nil)
	validFrom := common.BigToAddress(big.NewInt(1)).Bytes()

	testCases := []struct {
		msg        string
		to         *common.Address
		amount     *big.Int
		gasLimit   uint64
		gasPrice   *big.Int
		gasFeeCap  *big.Int
		gasTipCap  *big.Int
		from       []byte
		accessList *ethtypes.AccessList
		chainID    *big.Int
		expectPass bool
	}{
		{
			msg:        "pass with recipient - Legacy Tx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   hundredInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			expectPass: true,
		},
		{
			msg:        "pass with recipient - AccessList Tx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   zeroInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			accessList: &ethtypes.AccessList{},
			chainID:    hundredInt,
			expectPass: true,
		},
		{
			msg:        "pass with recipient - DynamicFee Tx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   zeroInt,
			gasFeeCap:  hundredInt,
			gasTipCap:  zeroInt,
			accessList: &ethtypes.AccessList{},
			chainID:    hundredInt,
			expectPass: true,
		},
		{
			msg:        "pass contract - Legacy Tx",
			to:         nil,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   hundredInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			expectPass: true,
		},
		{
			msg:        "nil amount - Legacy Tx",
			to:         &suite.to,
			from:       validFrom,
			amount:     nil,
			gasLimit:   1000,
			gasPrice:   hundredInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			expectPass: true,
		},
		{
			msg:        "negative amount - Legacy Tx",
			to:         &suite.to,
			from:       validFrom,
			amount:     minusOneInt,
			gasLimit:   1000,
			gasPrice:   hundredInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			expectPass: false,
		},
		{
			msg:        "zero gas limit - Legacy Tx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   0,
			gasPrice:   hundredInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			expectPass: false,
		},
		{
			msg:        "nil gas price - Legacy Tx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   nil,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			expectPass: true,
		},
		{
			msg:        "negative gas price - Legacy Tx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   minusOneInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			expectPass: false,
		},
		{
			msg:        "zero gas price - Legacy Tx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   zeroInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			expectPass: true,
		},
		{
			msg:        "invalid from address - Legacy Tx",
			to:         &suite.to,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   zeroInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			from:       nil,
			expectPass: false,
		},
		{
			msg:        "out of bound gas fee - Legacy Tx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   exp_2_255,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			expectPass: false,
		},
		{
			msg:        "nil amount - AccessListTx",
			to:         &suite.to,
			from:       validFrom,
			amount:     nil,
			gasLimit:   1000,
			gasPrice:   hundredInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			accessList: &ethtypes.AccessList{},
			chainID:    hundredInt,
			expectPass: true,
		},
		{
			msg:        "negative amount - AccessListTx",
			to:         &suite.to,
			from:       validFrom,
			amount:     minusOneInt,
			gasLimit:   1000,
			gasPrice:   hundredInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			accessList: &ethtypes.AccessList{},
			chainID:    nil,
			expectPass: false,
		},
		{
			msg:        "zero gas limit - AccessListTx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   0,
			gasPrice:   zeroInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			accessList: &ethtypes.AccessList{},
			chainID:    hundredInt,
			expectPass: false,
		},
		{
			msg:        "nil gas price - AccessListTx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   nil,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			accessList: &ethtypes.AccessList{},
			chainID:    hundredInt,
			expectPass: true,
		},
		{
			msg:        "negative gas price - AccessListTx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   minusOneInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			accessList: &ethtypes.AccessList{},
			chainID:    nil,
			expectPass: false,
		},
		{
			msg:        "zero gas price - AccessListTx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   zeroInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			accessList: &ethtypes.AccessList{},
			chainID:    hundredInt,
			expectPass: true,
		},
		{
			msg:        "invalid from address - AccessListTx",
			to:         &suite.to,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   zeroInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			from:       nil,
			accessList: &ethtypes.AccessList{},
			chainID:    hundredInt,
			expectPass: false,
		},
		{
			msg:        "chain ID not set on AccessListTx",
			to:         &suite.to,
			from:       validFrom,
			amount:     hundredInt,
			gasLimit:   1000,
			gasPrice:   zeroInt,
			gasFeeCap:  nil,
			gasTipCap:  nil,
			accessList: &ethtypes.AccessList{},
			chainID:    nil,
			expectPass: true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.msg, func() {
			tx := types.NewTx(tc.chainID, 1, tc.to, tc.amount, tc.gasLimit, tc.gasPrice, tc.gasFeeCap, tc.gasTipCap, nil, tc.accessList)
			tx.From = tc.from

			bz, err := tx.Marshal()
			if err != nil && !tc.expectPass {
				return
			}
			suite.Require().NoError(err)

			var tx2 types.MsgEthereumTx
			tx2.Unmarshal(bz)
			err = tx2.ValidateBasic()
			if tc.expectPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}

			if tc.expectPass {
				bz2, err := tx2.Marshal()
				suite.Require().NoError(err)

				suite.Require().Equal(bz, bz2)
			}
		})
	}
}

func (suite *MsgsTestSuite) TestMsgEthereumTx_ValidateBasicAdvanced() {
	hundredInt := big.NewInt(100)
	testCases := []struct {
		msg        string
		msgBuilder func() *types.MsgEthereumTx
		expectPass bool
	}{
		{
			"fails - invalid tx hash",
			func() *types.MsgEthereumTx {
				msg := types.NewTxContract(
					hundredInt,
					1,
					big.NewInt(10),
					100000,
					big.NewInt(150),
					big.NewInt(200),
					nil,
					nil,
					nil,
				)
				msg.DeprecatedHash = "0x00"
				return msg
			},
			false,
		},
		{
			"fails - invalid size",
			func() *types.MsgEthereumTx {
				msg := types.NewTxContract(
					hundredInt,
					1,
					big.NewInt(10),
					100000,
					big.NewInt(150),
					big.NewInt(200),
					nil,
					nil,
					nil,
				)
				msg.Size_ = 1
				return msg
			},
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.msg, func() {
			err := tc.msgBuilder().ValidateBasic()
			if tc.expectPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

func (suite *MsgsTestSuite) TestMsgEthereumTx_Sign() {
	testCases := []struct {
		msg        string
		tx         *types.MsgEthereumTx
		ethSigner  ethtypes.Signer
		malleate   func(tx *types.MsgEthereumTx)
		expectPass bool
	}{
		{
			"pass - EIP2930 signer",
			types.NewTx(suite.chainID, 0, &suite.to, nil, 100000, nil, nil, nil, []byte("test"), &ethtypes.AccessList{}),
			ethtypes.NewEIP2930Signer(suite.chainID),
			func(tx *types.MsgEthereumTx) { tx.From = suite.from.Bytes() },
			true,
		},
		{
			"pass - EIP155 signer",
			types.NewTx(suite.chainID, 0, &suite.to, nil, 100000, nil, nil, nil, []byte("test"), nil),
			ethtypes.NewEIP155Signer(suite.chainID),
			func(tx *types.MsgEthereumTx) { tx.From = suite.from.Bytes() },
			true,
		},
		{
			"pass - Homestead signer",
			types.NewTx(suite.chainID, 0, &suite.to, nil, 100000, nil, nil, nil, []byte("test"), nil),
			ethtypes.HomesteadSigner{},
			func(tx *types.MsgEthereumTx) { tx.From = suite.from.Bytes() },
			true,
		},
		{
			"pass - Frontier signer",
			types.NewTx(suite.chainID, 0, &suite.to, nil, 100000, nil, nil, nil, []byte("test"), nil),
			ethtypes.FrontierSigner{},
			func(tx *types.MsgEthereumTx) { tx.From = suite.from.Bytes() },
			true,
		},
		{
			"no from address ",
			types.NewTx(suite.chainID, 0, &suite.to, nil, 100000, nil, nil, nil, []byte("test"), &ethtypes.AccessList{}),
			ethtypes.NewEIP2930Signer(suite.chainID),
			func(tx *types.MsgEthereumTx) { tx.From = nil },
			false,
		},
		{
			"from address ≠ signer address",
			types.NewTx(suite.chainID, 0, &suite.to, nil, 100000, nil, nil, nil, []byte("test"), &ethtypes.AccessList{}),
			ethtypes.NewEIP2930Signer(suite.chainID),
			func(tx *types.MsgEthereumTx) { tx.From = suite.to.Bytes() },
			false,
		},
	}

	for i, tc := range testCases {
		tc.malleate(tc.tx)

		err := tc.tx.Sign(tc.ethSigner, suite.signer)
		if tc.expectPass {
			suite.Require().NoError(err, "valid test %d failed: %s", i, tc.msg)

			suite.Require().NoError(err, tc.msg)
			suite.Require().NoError(tc.tx.VerifySender(ethtypes.LatestSignerForChainID(suite.chainID)))
		} else {
			suite.Require().Error(err, "invalid test %d passed: %s", i, tc.msg)
		}
	}
}

func (suite *MsgsTestSuite) TestMsgEthereumTx_Getters() {
	testCases := []struct {
		name      string
		tx        *types.MsgEthereumTx
		ethSigner ethtypes.Signer
		exp       *big.Int
	}{
		{
			"get fee - pass",
			types.NewTx(suite.chainID, 0, &suite.to, nil, 50, suite.hundredBigInt, nil, nil, nil, &ethtypes.AccessList{}),
			ethtypes.NewEIP2930Signer(suite.chainID),
			big.NewInt(5000),
		},
		{
			"get effective fee - pass",
			types.NewTx(suite.chainID, 0, &suite.to, nil, 50, suite.hundredBigInt, nil, nil, nil, &ethtypes.AccessList{}),
			ethtypes.NewEIP2930Signer(suite.chainID),
			big.NewInt(5000),
		},
		{
			"get gas - pass",
			types.NewTx(suite.chainID, 0, &suite.to, nil, 50, suite.hundredBigInt, nil, nil, nil, &ethtypes.AccessList{}),
			ethtypes.NewEIP2930Signer(suite.chainID),
			big.NewInt(50),
		},
	}

	var fee, effFee *big.Int
	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			if strings.Contains(tc.name, "get fee") {
				fee = tc.tx.GetFee()
				suite.Require().Equal(tc.exp, fee)
			} else if strings.Contains(tc.name, "get effective fee") {
				effFee = tc.tx.GetEffectiveFee(big.NewInt(0))
				suite.Require().Equal(tc.exp, effFee)
			} else if strings.Contains(tc.name, "get gas") {
				gas := tc.tx.GetGas()
				suite.Require().Equal(tc.exp.Uint64(), gas)
			}
		})
	}
}

func (suite *MsgsTestSuite) TestFromEthereumTx() {
	privkey, _ := ethsecp256k1.GenerateKey()
	ethPriv, err := privkey.ToECDSA()
	suite.Require().NoError(err)

	// 10^80 is more than 256 bits
	exp_10_80 := new(big.Int).Exp(big.NewInt(10), big.NewInt(80), nil)

	testCases := []struct {
		msg        string
		expectPass bool
		buildTx    func() *ethtypes.Transaction
	}{
		{"success, normal tx", true, func() *ethtypes.Transaction {
			tx := ethtypes.NewTx(&ethtypes.AccessListTx{
				Nonce:    0,
				Data:     nil,
				To:       &suite.to,
				Value:    big.NewInt(10),
				GasPrice: big.NewInt(1),
				Gas:      21000,
			})
			tx, err := ethtypes.SignTx(tx, ethtypes.NewEIP2930Signer(suite.chainID), ethPriv)
			suite.Require().NoError(err)
			return tx
		}},
		{"success, DynamicFeeTx", true, func() *ethtypes.Transaction {
			tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
				Nonce: 0,
				Data:  nil,
				To:    &suite.to,
				Value: big.NewInt(10),
				Gas:   21000,
			})
			tx, err := ethtypes.SignTx(tx, ethtypes.NewLondonSigner(suite.chainID), ethPriv)
			suite.Require().NoError(err)
			return tx
		}},
		{"success, value bigger than 256bits - AccessListTx", true, func() *ethtypes.Transaction {
			tx := ethtypes.NewTx(&ethtypes.AccessListTx{
				Nonce:    0,
				Data:     nil,
				To:       &suite.to,
				Value:    exp_10_80,
				GasPrice: big.NewInt(1),
				Gas:      21000,
			})
			tx, err := ethtypes.SignTx(tx, ethtypes.NewEIP2930Signer(suite.chainID), ethPriv)
			suite.Require().NoError(err)
			return tx
		}},
		{"success, gas price bigger than 256bits - AccessListTx", true, func() *ethtypes.Transaction {
			tx := ethtypes.NewTx(&ethtypes.AccessListTx{
				Nonce:    0,
				Data:     nil,
				To:       &suite.to,
				Value:    big.NewInt(1),
				GasPrice: exp_10_80,
				Gas:      21000,
			})
			tx, err := ethtypes.SignTx(tx, ethtypes.NewEIP2930Signer(suite.chainID), ethPriv)
			suite.Require().NoError(err)
			return tx
		}},
		{"success, value bigger than 256bits - LegacyTx", true, func() *ethtypes.Transaction {
			tx := ethtypes.NewTx(&ethtypes.LegacyTx{
				Nonce:    0,
				Data:     nil,
				To:       &suite.to,
				Value:    exp_10_80,
				GasPrice: big.NewInt(1),
				Gas:      21000,
			})
			tx, err := ethtypes.SignTx(tx, ethtypes.NewEIP2930Signer(suite.chainID), ethPriv)
			suite.Require().NoError(err)
			return tx
		}},
		{"success, gas price bigger than 256bits - LegacyTx", true, func() *ethtypes.Transaction {
			tx := ethtypes.NewTx(&ethtypes.LegacyTx{
				Nonce:    0,
				Data:     nil,
				To:       &suite.to,
				Value:    big.NewInt(1),
				GasPrice: exp_10_80,
				Gas:      21000,
			})
			tx, err := ethtypes.SignTx(tx, ethtypes.NewEIP2930Signer(suite.chainID), ethPriv)
			suite.Require().NoError(err)
			return tx
		}},
	}

	for _, tc := range testCases {
		suite.Run(tc.msg, func() {
			tx := types.MsgEthereumTx{
				Raw: types.EthereumTx{
					Transaction: tc.buildTx(),
				},
			}

			// round-trip encoding
			bz, err := tx.Marshal()
			suite.Require().NoError(err)
			var tx2 types.MsgEthereumTx
			suite.Require().NoError(tx2.Unmarshal(bz))

			err = assertEqual(tx.Raw.Transaction, tx2.Raw.Transaction)
			if tc.expectPass {
				suite.Require().NoError(err)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}

// TestTransactionCoding tests serializing/de-serializing to/from rlp and JSON.
// adapted from go-ethereum
func (suite *MsgsTestSuite) TestTransactionCoding() {
	key, err := crypto.GenerateKey()
	if err != nil {
		suite.T().Fatalf("could not generate key: %v", err)
	}
	var (
		signer    = ethtypes.NewEIP2930Signer(common.Big1)
		addr      = common.HexToAddress("0x0000000000000000000000000000000000000001")
		recipient = common.HexToAddress("095e7baea6a6c7c4c2dfeb977efac326af552d87")
		accesses  = ethtypes.AccessList{{Address: addr, StorageKeys: []common.Hash{{0}}}}
	)
	for i := uint64(0); i < 500; i++ {
		var txdata ethtypes.TxData
		switch i % 5 {
		case 0:
			// Legacy tx.
			txdata = &ethtypes.LegacyTx{
				Nonce:    i,
				To:       &recipient,
				Gas:      1,
				GasPrice: big.NewInt(2),
				Data:     []byte("abcdef"),
			}
		case 1:
			// Legacy tx contract creation.
			txdata = &ethtypes.LegacyTx{
				Nonce:    i,
				Gas:      1,
				GasPrice: big.NewInt(2),
				Data:     []byte("abcdef"),
			}
		case 2:
			// Tx with non-zero access list.
			txdata = &ethtypes.AccessListTx{
				ChainID:    big.NewInt(1),
				Nonce:      i,
				To:         &recipient,
				Gas:        123457,
				GasPrice:   big.NewInt(10),
				AccessList: accesses,
				Data:       []byte("abcdef"),
			}
		case 3:
			// Tx with empty access list.
			txdata = &ethtypes.AccessListTx{
				ChainID:  big.NewInt(1),
				Nonce:    i,
				To:       &recipient,
				Gas:      123457,
				GasPrice: big.NewInt(10),
				Data:     []byte("abcdef"),
			}
		case 4:
			// Contract creation with access list.
			txdata = &ethtypes.AccessListTx{
				ChainID:    big.NewInt(1),
				Nonce:      i,
				Gas:        123457,
				GasPrice:   big.NewInt(10),
				AccessList: accesses,
			}
		}
		tx, err := ethtypes.SignNewTx(key, signer, txdata)
		if err != nil {
			suite.T().Fatalf("could not sign transaction: %v", err)
		}
		// RLP
		parsedTx, err := encodeDecodeBinary(tx, signer.ChainID())
		if err != nil {
			suite.T().Fatal(err)
		}
		assertEqual(parsedTx.Raw.Transaction, tx)
	}
}

func encodeDecodeBinary(tx *ethtypes.Transaction, chainID *big.Int) (*types.MsgEthereumTx, error) {
	data, err := tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("rlp encoding failed: %v", err)
	}
	parsedTx := &types.MsgEthereumTx{}
	if err := parsedTx.UnmarshalBinary(data, ethtypes.LatestSignerForChainID(chainID)); err != nil {
		return nil, fmt.Errorf("rlp decoding failed: %v", err)
	}
	return parsedTx, nil
}

func assertEqual(orig *ethtypes.Transaction, cpy *ethtypes.Transaction) error {
	// compare nonce, price, gaslimit, recipient, amount, payload, V, R, S
	if want, got := orig.Hash(), cpy.Hash(); want != got {
		return fmt.Errorf("parsed tx differs from original tx, want %v, got %v", want, got)
	}
	if want, got := orig.ChainId(), cpy.ChainId(); want.Cmp(got) != 0 {
		return fmt.Errorf("invalid chain id, want %d, got %d", want, got)
	}
	if orig.AccessList() != nil {
		if !reflect.DeepEqual(orig.AccessList(), cpy.AccessList()) {
			return fmt.Errorf("access list wrong")
		}
	}
	return nil
}

func (suite *MsgsTestSuite) TestValidateEthereumTx() {
	maxInt256 := ethermint.MaxInt256
	maxInt256Plus1 := new(big.Int).Add(ethermint.MaxInt256, big.NewInt(1))
	normal := big.NewInt(100)
	gasLimit := uint64(21000)
	testCases := []struct {
		name     string
		tx       types.EthereumTx
		expError bool
	}{
		{
			"valid transaction",
			types.NewTxWithData(&ethtypes.LegacyTx{
				Gas:      gasLimit,
				GasPrice: normal,
				Value:    normal,
			}).Raw,
			false,
		},
		{
			"zero gas limit",
			types.NewTxWithData(&ethtypes.LegacyTx{
				Gas:      0,
				GasPrice: normal,
				Value:    normal,
			}).Raw,
			true,
		},
		{
			"gas price exceeds int256 limit",
			types.NewTxWithData(&ethtypes.LegacyTx{
				Value:    normal,
				Gas:      gasLimit,
				GasPrice: maxInt256Plus1,
			}).Raw,
			true,
		},
		{
			"gas fee cap exceeds int256 limit",
			types.NewTxWithData(&ethtypes.DynamicFeeTx{
				Value:     normal,
				Gas:       gasLimit,
				GasFeeCap: maxInt256Plus1,
			}).Raw,
			true,
		},
		{
			"gas tip cap exceeds int256 limit",
			types.NewTxWithData(&ethtypes.DynamicFeeTx{
				Value:     normal,
				Gas:       gasLimit,
				GasFeeCap: normal,
				GasTipCap: maxInt256Plus1,
			}).Raw,
			true,
		},
		{
			"LegacyTx cost exceeds int256 limit",
			types.NewTxWithData(&ethtypes.LegacyTx{
				Gas:      gasLimit,
				GasPrice: maxInt256,
				Value:    normal,
			}).Raw,
			true,
		},
		{
			"DynamicFeeTx cost exceeds int256 limit",
			types.NewTxWithData(&ethtypes.DynamicFeeTx{
				Gas:   gasLimit,
				Value: maxInt256Plus1,
			}).Raw,
			true,
		},
		{
			"AccessListTx cost exceeds int256 limit",
			types.NewTxWithData(&ethtypes.AccessListTx{
				Gas:      gasLimit,
				GasPrice: maxInt256,
				Value:    normal,
			}).Raw,
			true,
		},
	}
	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := tc.tx.Validate()
			if tc.expError {
				suite.Require().Error(err, tc.name)
			} else {
				suite.Require().NoError(err, tc.name)
			}
		})
	}
}
