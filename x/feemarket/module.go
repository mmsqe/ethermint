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
package feemarket

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	"cosmossdk.io/core/appmodule"
	coreregistry "cosmossdk.io/core/registry"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"github.com/evmos/ethermint/x/feemarket/client/cli"
	"github.com/evmos/ethermint/x/feemarket/keeper"
	"github.com/evmos/ethermint/x/feemarket/simulation"
	"github.com/evmos/ethermint/x/feemarket/types"
)

var (
	_ module.AppModule           = AppModule{}
	_ module.AppModuleSimulation = (*AppModule)(nil)
	_ module.HasGenesis          = (*AppModule)(nil)
	_ module.HasServices         = (*AppModule)(nil)
	_ appmodule.AppModule        = (*AppModule)(nil)
	_ appmodule.HasEndBlocker    = AppModule{}
	_ appmodule.HasBeginBlocker  = AppModule{}
)

// Name returns the fee market module's name.
func (AppModule) Name() string {
	return types.ModuleName
}

// RegisterLegacyAminoCodec performs a no-op as the fee market module doesn't support amino.
func (AppModule) RegisterLegacyAminoCodec(cdc coreregistry.AminoRegistrar) {
	types.RegisterLegacyAminoCodec(cdc)
}

// ConsensusVersion returns the consensus state-breaking version for the module.
func (AppModule) ConsensusVersion() uint64 {
	return 4
}

// DefaultGenesis returns default genesis state as raw bytes for the fee market
// module.
func (am AppModule) DefaultGenesis() json.RawMessage {
	return am.cdc.MustMarshalJSON(types.DefaultGenesisState())
}

// ValidateGenesis is the validation check of the Genesis
func (am AppModule) ValidateGenesis(bz json.RawMessage) error {
	var genesisState types.GenesisState
	if err := am.cdc.UnmarshalJSON(bz, &genesisState); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}

	return genesisState.Validate()
}

// RegisterRESTRoutes performs a no-op as the EVM module doesn't expose REST
// endpoints
func (AppModule) RegisterRESTRoutes(_ client.Context, _ *mux.Router) {
}

func (AppModule) RegisterGRPCGatewayRoutes(c client.Context, serveMux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), serveMux, types.NewQueryClient(c)); err != nil {
		panic(err)
	}
}

// RegisterInterfaces registers interfaces and implementations of the fee market module.
func (AppModule) RegisterInterfaces(registry coreregistry.InterfaceRegistrar) {
	types.RegisterInterfaces(registry)
}

// GetTxCmd returns the root tx command for the fee market module.
func (AppModule) GetTxCmd() *cobra.Command {
	return nil
}

// GetQueryCmd returns no root query command for the fee market module.
func (AppModule) GetQueryCmd() *cobra.Command {
	return cli.GetQueryCmd()
}

// ____________________________________________________________________________

// AppModule implements an application module for the fee market module.
type AppModule struct {
	cdc    codec.Codec
	keeper keeper.Keeper
	// legacySubspace is used solely for migration of x/params managed parameters
	legacySubspace types.Subspace
}

// NewAppModule creates a new AppModule object
func NewAppModule(cdc codec.Codec, k keeper.Keeper, ss types.Subspace) AppModule {
	return AppModule{
		cdc:            cdc,
		keeper:         k,
		legacySubspace: ss,
	}
}

// RegisterInvariants interface for registering invariants. Performs a no-op
// as the fee market module doesn't expose invariants.
func (am AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// RegisterServices registers the GRPC query service and migrator service to respond to the
// module-specific GRPC queries and handle the upgrade store migration for the module.
func (am AppModule) RegisterServices(cfg module.Configurator) { //nolint:staticcheck // SA1019: Configurator is still used in runtime v1.
	types.RegisterQueryServer(cfg.QueryServer(), am.keeper)
	types.RegisterMsgServer(cfg.MsgServer(), &am.keeper)

	m := keeper.NewMigrator(am.keeper, am.legacySubspace)
	if err := cfg.RegisterMigration(types.ModuleName, 3, m.Migrate3to4); err != nil {
		panic(err)
	}
}

// BeginBlock returns the begin block for the fee market module.
func (am AppModule) BeginBlock(ctx context.Context) error {
	return am.keeper.BeginBlock(sdk.UnwrapSDKContext(ctx))
}

// EndBlock returns the end blocker for the fee market module. It returns no validator
// updates.
func (am AppModule) EndBlock(ctx context.Context) error {
	return am.keeper.EndBlock(sdk.UnwrapSDKContext(ctx))
}

// InitGenesis performs genesis initialization for the fee market module. It returns
// no validator updates.
func (am AppModule) InitGenesis(ctx context.Context, data json.RawMessage) error {
	var genesisState types.GenesisState
	am.cdc.MustUnmarshalJSON(data, &genesisState)
	InitGenesis(ctx, am.keeper, genesisState)
	return nil
}

// ExportGenesis returns the exported genesis state as raw bytes for the fee market
// module.
func (am AppModule) ExportGenesis(ctx context.Context) (json.RawMessage, error) {
	gs := ExportGenesis(ctx, am.keeper)
	return am.cdc.MarshalJSON(gs)
}

// RegisterStoreDecoder registers a decoder for fee market module's types
func (am AppModule) RegisterStoreDecoder(_ simtypes.StoreDecoderRegistry) {}

// GenerateGenesisState creates a randomized GenState of the fee market module.
func (AppModule) GenerateGenesisState(simState *module.SimulationState) {
	simulation.RandomizedGenState(simState)
}

// WeightedOperations returns the all the fee market module operations with their respective weights.
func (am AppModule) WeightedOperations(_ module.SimulationState) []simtypes.WeightedOperation {
	return nil
}

// IsAppModule implements the appmodule.AppModule interface.
func (am AppModule) IsAppModule() {}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am AppModule) IsOnePerModuleType() {}
