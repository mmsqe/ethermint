package backend

import (
	"fmt"
	"math/big"

	"github.com/cometbft/cometbft/abci/types"
	tmrpctypes "github.com/cometbft/cometbft/rpc/core/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/evmos/ethermint/rpc/backend/mocks"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	feemarkettypes "github.com/evmos/ethermint/x/feemarket/types"
)

func (suite *BackendTestSuite) TestBaseFee() {
	baseFee := sdk.NewInt(1)

	testCases := []struct {
		name         string
		blockRes     *tmrpctypes.ResultBlockResults
		registerMock func()
		expBaseFee   *big.Int
		expPass      bool
	}{
		{
			"fail - grpc BaseFee error",
			&tmrpctypes.ResultBlockResults{Height: 1},
			func() {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBaseFeeError(queryClient)
			},
			nil,
			false,
		},
		{
			"fail - grpc BaseFee error - with non feemarket block event",
			&tmrpctypes.ResultBlockResults{
				Height: 1,
				BeginBlockEvents: []types.Event{
					{
						Type: evmtypes.EventTypeBlockBloom,
					},
				},
			},
			func() {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBaseFeeError(queryClient)
			},
			nil,
			false,
		},
		{
			"fail - grpc BaseFee error - with feemarket block event",
			&tmrpctypes.ResultBlockResults{
				Height: 1,
				BeginBlockEvents: []types.Event{
					{
						Type: feemarkettypes.EventTypeFeeMarket,
					},
				},
			},
			func() {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBaseFeeError(queryClient)
			},
			nil,
			false,
		},
		{
			"fail - grpc BaseFee error - with feemarket block event with wrong attribute value",
			&tmrpctypes.ResultBlockResults{
				Height: 1,
				BeginBlockEvents: []types.Event{
					{
						Type: feemarkettypes.EventTypeFeeMarket,
						Attributes: []types.EventAttribute{
							{Value: "/1"},
						},
					},
				},
			},
			func() {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBaseFeeError(queryClient)
			},
			nil,
			false,
		},
		{
			"fail - grpc baseFee error - with feemarket block event with baseFee attribute value",
			&tmrpctypes.ResultBlockResults{
				Height: 1,
				BeginBlockEvents: []types.Event{
					{
						Type: feemarkettypes.EventTypeFeeMarket,
						Attributes: []types.EventAttribute{
							{Value: baseFee.String()},
						},
					},
				},
			},
			func() {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBaseFeeError(queryClient)
			},
			baseFee.BigInt(),
			true,
		},
		{
			"fail - base fee or london fork not enabled",
			&tmrpctypes.ResultBlockResults{Height: 1},
			func() {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBaseFeeDisabled(queryClient)
			},
			nil,
			true,
		},
		{
			"pass",
			&tmrpctypes.ResultBlockResults{Height: 1},
			func() {
				queryClient := suite.backend.queryClient.QueryClient.(*mocks.EVMQueryClient)
				RegisterBaseFee(queryClient, baseFee)
			},
			baseFee.BigInt(),
			true,
		},
	}
	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest() // reset test and queries
			tc.registerMock()

			baseFee, err := suite.backend.BaseFee(tc.blockRes)

			if tc.expPass {
				suite.Require().NoError(err)
				suite.Require().Equal(tc.expBaseFee, baseFee)
			} else {
				suite.Require().Error(err)
			}
		})
	}
}
