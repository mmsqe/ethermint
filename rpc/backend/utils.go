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
package backend

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"cosmossdk.io/log"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"

	abci "github.com/cometbft/cometbft/abci/types"
	tmrpctypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"

	crypto "github.com/cometbft/cometbft/api/cometbft/crypto/v1"
	"github.com/evmos/ethermint/rpc/types"
	ethermint "github.com/evmos/ethermint/types"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	feemarkettypes "github.com/evmos/ethermint/x/feemarket/types"
)

type txGasAndReward struct {
	gasUsed uint64
	reward  *big.Int
}

type sortGasAndReward []txGasAndReward

func (s sortGasAndReward) Len() int { return len(s) }
func (s sortGasAndReward) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s sortGasAndReward) Less(i, j int) bool {
	return s[i].reward.Cmp(s[j].reward) < 0
}

// getAccountNonce returns the account nonce for the given account address.
// If the pending value is true, it will iterate over the mempool (pending)
// txs in order to compute and return the pending tx sequence.
// Todo: include the ability to specify a blockNumber
func (b *Backend) getAccountNonce(accAddr common.Address, pending bool, height int64, logger log.Logger) (uint64, error) {
	queryClient := authtypes.NewQueryClient(b.clientCtx)
	adr := sdk.AccAddress(accAddr.Bytes()).String()
	ctx := types.ContextWithHeight(height)
	res, err := queryClient.Account(ctx, &authtypes.QueryAccountRequest{Address: adr})
	if err != nil {
		st, ok := status.FromError(err)
		// treat as account doesn't exist yet
		if ok && st.Code() == codes.NotFound {
			return 0, nil
		}
		return 0, err
	}
	var acc sdk.AccountI
	if err := b.clientCtx.InterfaceRegistry.UnpackAny(res.Account, &acc); err != nil {
		return 0, err
	}

	nonce := acc.GetSequence()

	if !pending {
		return nonce, nil
	}

	// the account retriever doesn't include the uncommitted transactions on the nonce so we need to
	// to manually add them.
	pendingTxs, err := b.PendingTransactions()
	if err != nil {
		logger.Error("failed to fetch pending transactions", "error", err.Error())
		return nonce, nil
	}

	// add the uncommitted txs to the nonce counter
	// only supports `MsgEthereumTx` style tx
	for _, tx := range pendingTxs {
		for _, msg := range (*tx).GetMsgs() {
			ethMsg, ok := msg.(*evmtypes.MsgEthereumTx)
			if !ok {
				// not ethereum tx
				break
			}

			sender, err := ethMsg.GetSenderLegacy(ethtypes.LatestSignerForChainID(b.chainID))
			if err != nil {
				continue
			}
			if sender == accAddr {
				nonce++
			}
		}
	}

	return nonce, nil
}

// CalcBaseFee calculates the basefee of the header.
func CalcBaseFee(config *params.ChainConfig, parent *ethtypes.Header, p feemarkettypes.Params) (*big.Int, error) {
	// If the current block is the first EIP-1559 block, return the InitialBaseFee.
	if !config.IsLondon(parent.Number) {
		return new(big.Int).SetUint64(params.InitialBaseFee), nil
	}
	if p.ElasticityMultiplier == 0 {
		return nil, errors.New("ElasticityMultiplier cannot be 0 as it's checked in the params validation")
	}
	parentGasTarget := parent.GasLimit / uint64(p.ElasticityMultiplier)
	// If the parent gasUsed is the same as the target, the baseFee remains unchanged.
	if parent.GasUsed == parentGasTarget {
		return new(big.Int).Set(parent.BaseFee), nil
	}

	var (
		num   = new(big.Int)
		denom = new(big.Int)
	)

	if parent.GasUsed > parentGasTarget {
		// If the parent block used more gas than its target, the baseFee should increase.
		// max(1, parentBaseFee * gasUsedDelta / parentGasTarget / baseFeeChangeDenominator)
		num.SetUint64(parent.GasUsed - parentGasTarget)
		num.Mul(num, parent.BaseFee)
		num.Div(num, denom.SetUint64(parentGasTarget))
		num.Div(num, denom.SetUint64(uint64(p.BaseFeeChangeDenominator)))
		baseFeeDelta := math.BigMax(num, common.Big1)

		return num.Add(parent.BaseFee, baseFeeDelta), nil
	}

	// Otherwise if the parent block used less gas than its target, the baseFee should decrease.
	// max(0, parentBaseFee * gasUsedDelta / parentGasTarget / baseFeeChangeDenominator)
	num.SetUint64(parentGasTarget - parent.GasUsed)
	num.Mul(num, parent.BaseFee)
	num.Div(num, denom.SetUint64(parentGasTarget))
	num.Div(num, denom.SetUint64(uint64(p.BaseFeeChangeDenominator)))
	baseFee := num.Sub(parent.BaseFee, num)
	minGasPrice := p.MinGasPrice.TruncateInt().BigInt()
	return math.BigMax(baseFee, minGasPrice), nil
}

// output: targetOneFeeHistory
func (b *Backend) processBlock(
	tendermintBlock *tmrpctypes.ResultBlock,
	ethBlock *map[string]interface{},
	rewardPercentiles []float64,
	tendermintBlockResult *tmrpctypes.ResultBlockResults,
	targetOneFeeHistory *types.OneFeeHistory,
) error {
	blockHeight := tendermintBlock.Block.Height
	blockBaseFee, err := b.BaseFee(tendermintBlockResult)
	if err != nil || blockBaseFee == nil {
		targetOneFeeHistory.BaseFee = big.NewInt(0)
	} else {
		targetOneFeeHistory.BaseFee = blockBaseFee
	}
	cfg := b.ChainConfig()
	// set gas used ratio
	gasLimitUint64, ok := (*ethBlock)["gasLimit"].(hexutil.Uint64)
	if !ok {
		return fmt.Errorf("invalid gas limit type: %T", (*ethBlock)["gasLimit"])
	}

	gasUsedBig, ok := (*ethBlock)["gasUsed"].(*hexutil.Big)
	if !ok {
		return fmt.Errorf("invalid gas used type: %T", (*ethBlock)["gasUsed"])
	}

	if cfg.IsLondon(big.NewInt(blockHeight + 1)) {
		var header ethtypes.Header
		header.Number = new(big.Int).SetInt64(blockHeight)
		baseFee, ok := (*ethBlock)["baseFeePerGas"].(*hexutil.Big)
		if !ok || baseFee == nil {
			header.BaseFee = big.NewInt(0)
		} else {
			header.BaseFee = baseFee.ToInt()
		}
		header.GasLimit = uint64(gasLimitUint64)
		header.GasUsed = gasUsedBig.ToInt().Uint64()
		ctx := types.ContextWithHeight(blockHeight)
		params, err := b.queryClient.FeeMarket.Params(ctx, &feemarkettypes.QueryParamsRequest{})
		if err != nil {
			return err
		}
		nextBaseFee, err := CalcBaseFee(cfg, &header, params.Params)
		if err != nil {
			return err
		}
		targetOneFeeHistory.NextBaseFee = nextBaseFee
	} else {
		targetOneFeeHistory.NextBaseFee = new(big.Int)
	}
	gasusedfloat, _ := new(big.Float).SetInt(gasUsedBig.ToInt()).Float64()

	if gasLimitUint64 <= 0 {
		return fmt.Errorf("gasLimit of block height %d should be bigger than 0 , current gaslimit %d", blockHeight, gasLimitUint64)
	}

	gasUsedRatio := gasusedfloat / float64(gasLimitUint64)
	blockGasUsed := gasusedfloat
	targetOneFeeHistory.GasUsedRatio = gasUsedRatio

	rewardCount := len(rewardPercentiles)
	targetOneFeeHistory.Reward = make([]*big.Int, rewardCount)
	for i := 0; i < rewardCount; i++ {
		targetOneFeeHistory.Reward[i] = big.NewInt(0)
	}

	// check tendermintTxs
	tendermintTxs := tendermintBlock.Block.Txs
	tendermintTxResults := tendermintBlockResult.TxsResults
	tendermintTxCount := len(tendermintTxs)

	var sorter sortGasAndReward

	for i := 0; i < tendermintTxCount; i++ {
		eachTendermintTx := tendermintTxs[i]
		eachTendermintTxResult := tendermintTxResults[i]

		tx, err := b.clientCtx.TxConfig.TxDecoder()(eachTendermintTx)
		if err != nil {
			b.logger.Debug("failed to decode transaction in block", "height", blockHeight, "error", err.Error())
			continue
		}
		txGasUsed, err := ethermint.SafeUint64(eachTendermintTxResult.GasUsed)
		if err != nil {
			return err
		}
		for _, msg := range tx.GetMsgs() {
			ethMsg, ok := msg.(*evmtypes.MsgEthereumTx)
			if !ok {
				continue
			}
			tx := ethMsg.AsTransaction()
			reward := tx.EffectiveGasTipValue(blockBaseFee)
			if reward == nil {
				reward = big.NewInt(0)
			}
			sorter = append(sorter, txGasAndReward{gasUsed: txGasUsed, reward: reward})
		}
	}

	// return an all zero row if there are no transactions to gather data from
	ethTxCount := len(sorter)
	if ethTxCount == 0 {
		return nil
	}

	sort.Sort(sorter)

	var txIndex int
	sumGasUsed := sorter[0].gasUsed

	for i, p := range rewardPercentiles {
		thresholdGasUsed := uint64(blockGasUsed * p / 100)
		for sumGasUsed < thresholdGasUsed && txIndex < ethTxCount-1 {
			txIndex++
			sumGasUsed += sorter[txIndex].gasUsed
		}
		targetOneFeeHistory.Reward[i] = sorter[txIndex].reward
	}

	return nil
}

// ShouldIgnoreGasUsed returns true if the gasUsed in result should be ignored
// workaround for issue: https://github.com/cosmos/cosmos-sdk/issues/10832
func ShouldIgnoreGasUsed(res *abci.ExecTxResult) bool {
	return res.GetCode() == 11 && strings.Contains(res.GetLog(), "no block gas left to run tx: out of gas")
}

// GetLogsFromBlockResults returns the list of event logs from the tendermint block result response
func GetLogsFromBlockResults(blockRes *tmrpctypes.ResultBlockResults) ([][]*ethtypes.Log, error) {
	height, err := ethermint.SafeUint64(blockRes.Height)
	if err != nil {
		return nil, err
	}
	blockLogs := [][]*ethtypes.Log{}
	for _, txResult := range blockRes.TxsResults {
		logs, err := evmtypes.DecodeTxLogsFromEvents(txResult.Data, txResult.Events, height)
		if err != nil {
			return nil, err
		}
		blockLogs = append(blockLogs, logs)
	}
	return blockLogs, nil
}

// GetHexProofs returns list of hex data of proof op
func GetHexProofs(proof *crypto.ProofOps) []string {
	if proof == nil {
		return []string{""}
	}
	proofs := []string{}
	// check for proof
	for _, p := range proof.Ops {
		proof := ""
		if len(p.Data) > 0 {
			proof = hexutil.Encode(p.Data)
		}
		proofs = append(proofs, proof)
	}
	return proofs
}

func (b *Backend) getValidatorAccount(header *cmttypes.Header) (sdk.AccAddress, error) {
	res, err := b.queryClient.ValidatorAccount(
		types.ContextWithHeight(header.Height),
		&evmtypes.QueryValidatorAccountRequest{
			ConsAddress: sdk.ConsAddress(header.ProposerAddress).String(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get validator account %w", err)
	}
	return sdk.AccAddressFromBech32(res.AccountAddress)
}
