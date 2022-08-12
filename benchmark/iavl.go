package benchmark

import (
	"fmt"
	"os"

	"github.com/cosmos/cosmos-sdk/store/iavl"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

var (
	ModulePrefix = "s/k:evm/"
)

func MockWritesIAVL(kvstore storetypes.CommitKVStore, blocks int, writesPerContract int) error {
	contracts := GenMockContracts()
	for b := 0; b < blocks; b++ {
		for _, c := range contracts {
			store := prefix.NewStore(kvstore, evmtypes.AddressStoragePrefix(c.Address))
			for k, v := range c.GenSlotUpdates(writesPerContract) {
				store.Set(k.Bytes(), v.Bytes())
				fmt.Println("blk: ", b, store.Get(k.Bytes()))
			}
		}
		kvstore.Commit()
	}
	return nil
}

func getDB(path, dbName, prefix, storeKeyName string) (store storetypes.CommitKVStore, storeDB *dbm.PrefixDB, err error) {
	db, err := dbm.NewGoLevelDB(dbName, path)
	if err != nil {
		return nil, nil, err
	}
	storeDB = dbm.NewPrefixDB(db, []byte(prefix))
	storeKey := storetypes.NewKVStoreKey(storeKeyName)
	id := storetypes.CommitID{}
	store, err = iavl.LoadStore(storeDB, log.NewNopLogger(), storeKey, id, false, iavl.DefaultIAVLCacheSize)
	return
}

func BenchIAVL() {
	dir, err := os.Getwd()
	fmt.Printf("dir path: %s\n", dir)
	path := fmt.Sprintf("%s/%s", dir, "dist")
	os.RemoveAll(path)

	store1, db1, err := getDB(path, "testing", ModulePrefix, "evm")
	if err != nil {
		panic(err)
	}
	defer db1.Close()

	store2, db2, err := getDB(path, "testing2", ModulePrefix, "evm")
	if err != nil {
		panic(err)
	}
	defer db2.Close()

	iter := store1.Iterator(nil, nil)
	for iter.Valid() {
		iter.Next()
		if k, v := iter.Key(), iter.Value(); k != nil && v != nil {
			store2.Set(k, v)
		}
	}
	iter.Close()
	store2.Commit()
}
