package keeper_test

import (
	_ "embed"
	"math/big"
	"testing"

	sdkmath "cosmossdk.io/math"
	stakingkeeper "cosmossdk.io/x/staking/keeper"
	"github.com/evmos/ethermint/testutil"
	"github.com/stretchr/testify/suite"
)

type KeeperTestSuite struct {
	testutil.BaseTestSuiteWithAccount
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

func (suite *KeeperTestSuite) SetupTest() {
	t := suite.T()
	suite.SetupAccount(t)
	suite.SetupTestWithCb(t, nil)
	validator := suite.BaseTestSuiteWithAccount.PostSetupValidator(t)
	validator = stakingkeeper.TestingUpdateValidator(suite.App.StakingKeeper, suite.Ctx, validator, true)
	valBz, err := suite.App.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.GetOperator())
	suite.Require().NoError(err)
	err = suite.App.StakingKeeper.Hooks().AfterValidatorCreated(suite.Ctx, valBz)
	suite.Require().NoError(err)
	err = suite.App.StakingKeeper.SetValidatorByConsAddr(suite.Ctx, validator)
	suite.Require().NoError(err)
	suite.App.StakingKeeper.SetValidator(suite.Ctx, validator)
}

func (suite *KeeperTestSuite) TestSetGetBlockGasWanted() {
	testCases := []struct {
		name     string
		malleate func()
		expGas   uint64
	}{
		{
			"with last block given",
			func() {
				suite.App.FeeMarketKeeper.SetBlockGasWanted(suite.Ctx, uint64(1000000))
			},
			uint64(1000000),
		},
	}
	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			tc.malleate()
			gas := suite.App.FeeMarketKeeper.GetBlockGasWanted(suite.Ctx)
			suite.Require().Equal(tc.expGas, gas, tc.name)
		})
	}
}

func (suite *KeeperTestSuite) TestSetGetGasFee() {
	testCases := []struct {
		name     string
		malleate func()
		expFee   *big.Int
	}{
		{
			"with last block given",
			func() {
				suite.App.FeeMarketKeeper.SetBaseFee(suite.Ctx, sdkmath.LegacyOneDec().BigInt())
			},
			sdkmath.LegacyOneDec().BigInt(),
		},
	}
	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			tc.malleate()
			fee := suite.App.FeeMarketKeeper.GetBaseFee(suite.Ctx)
			suite.Require().Equal(tc.expFee, fee, tc.name)
		})
	}
}
