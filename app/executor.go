package app

import (
	"sync"

	"cosmossdk.io/collections"
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	evmkeeper "github.com/evmos/ethermint/x/evm/keeper"

	sdk "github.com/cosmos/cosmos-sdk/types"

	blockstm "github.com/crypto-org-chain/go-block-stm"
)

type PreEstimateFunc func(txs [][]byte, workers int, txDecoder sdk.TxDecoder, ms storetypes.MultiStore) ([]sdk.Tx, []blockstm.MultiLocations)

func GetPreEstimateFunc(stores []storetypes.StoreKey, evmKeeper *evmkeeper.Keeper) PreEstimateFunc {
	return func(txs [][]byte, workers int, txDecoder sdk.TxDecoder, ms storetypes.MultiStore) ([]sdk.Tx, []blockstm.MultiLocations) {
		var authStore, bankStore int
		index := make(map[storetypes.StoreKey]int, len(stores))
		for i, k := range stores {
			switch k.Name() {
			case authtypes.StoreKey:
				authStore = i
			case banktypes.StoreKey:
				bankStore = i
			}
			index[k] = i
		}
		evmDenom := evmKeeper.GetParams(sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())).EvmDenom
		return preEstimates(txs, workers, authStore, bankStore, evmDenom, txDecoder)
	}
}

// preEstimates returns a static estimation of the written keys for each transaction.
// NOTE: make sure it sync with the latest sdk logic when sdk upgrade.
func preEstimates(txs [][]byte, workers, authStore, bankStore int, evmDenom string, txDecoder sdk.TxDecoder) ([]sdk.Tx, []blockstm.MultiLocations) {
	memTxs := make([]sdk.Tx, len(txs))
	estimates := make([]blockstm.MultiLocations, len(txs))

	job := func(start, end int) {
		for i := start; i < end; i++ {
			rawTx := txs[i]
			tx, err := txDecoder(rawTx)
			if err != nil {
				continue
			}
			memTxs[i] = tx

			feeTx, ok := tx.(sdk.FeeTx)
			if !ok {
				continue
			}
			feePayer := sdk.AccAddress(feeTx.FeePayer())

			// account key
			accKey, err := collections.EncodeKeyWithPrefix(
				authtypes.AddressStoreKeyPrefix,
				sdk.AccAddressKey,
				feePayer,
			)
			if err != nil {
				continue
			}

			// balance key
			balanceKey, err := collections.EncodeKeyWithPrefix(
				banktypes.BalancesPrefix,
				collections.PairKeyCodec(sdk.AccAddressKey, collections.StringKey),
				collections.Join(feePayer, evmDenom),
			)
			if err != nil {
				continue
			}

			estimates[i] = blockstm.MultiLocations{
				authStore: {accKey},
				bankStore: {balanceKey},
			}
		}
	}

	blockSize := len(txs)
	chunk := (blockSize + workers - 1) / workers
	var wg sync.WaitGroup
	for i := 0; i < blockSize; i += chunk {
		start := i
		end := min(i+chunk, blockSize)
		wg.Add(1)
		go func() {
			defer wg.Done()
			job(start, end)
		}()
	}
	wg.Wait()

	return memTxs, estimates
}
