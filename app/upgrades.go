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
package app

import (
	"context"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
)

func (app *EthermintApp) RegisterUpgradeHandlers() {
	planName := "sdk50"
	app.UpgradeKeeper.SetUpgradeHandler(planName,
		func(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			m, err := app.ModuleManager.RunMigrations(ctx, app.configurator, fromVM)
			if err != nil {
				return m, err
			}
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			{
				params := app.EvmKeeper.GetParams(sdkCtx)
				params.HeaderHashNum = evmtypes.DefaultHeaderHashNum
				if err := app.EvmKeeper.SetParams(sdkCtx, params); err != nil {
					return m, err
				}
			}
			return m, nil
		},
	)

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Sprintf("failed to read upgrade info from disk %s", err))
	}
	if !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		if upgradeInfo.Name == planName {
			app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storetypes.StoreUpgrades{
				Deleted: []string{"ibc"},
			}))
		}
	}
}
