// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

import { Pack } from "@thirdweb-dev/contracts/contracts/prebuilts/pack/Pack.sol";
import { ITokenBundle } from "@thirdweb-dev/contracts/contracts/extension/interface/ITokenBundle.sol";
import { Wallet } from "./utils/Wallet.sol";
import "./mocks/WETH9.sol";
import { Forwarder } from "@thirdweb-dev/contracts/contracts/infra/forwarder/Forwarder.sol";
import { TWRegistry } from "@thirdweb-dev/contracts/contracts/infra/TWRegistry.sol";
import { TWFactory } from "@thirdweb-dev/contracts/contracts/infra/TWFactory.sol";

contract TestPack is Wallet {
    string public constant NAME = "NAME";
    string public constant SYMBOL = "SYMBOL";
    string public constant CONTRACT_URI = "CONTRACT_URI";
    address public constant NATIVE_TOKEN = 0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE;
    string internal packUri = "ipfs://";

    Pack internal pack;
    Wallet internal tokenOwner;
    ITokenBundle.Token[] internal packContents;
    uint256[] internal numOfRewardUnits;

    event ProxyAddress(address indexed proxyAddress);

    function setUp(
        address _forwarder,
        address _factory,
        address _recipient
    ) public payable {
        address deployer = address(this);
        address royaltyRecipient = address(0x30001);
        uint128 royaltyBps = 500; // 5%

        address[] memory forwarders = new address[](1);
        forwarders[0] = _forwarder;
        string memory _contractType = "Pack";
        bytes memory _initializer = abi.encodeCall(
            Pack.initialize,
            (deployer, NAME, SYMBOL, CONTRACT_URI, forwarders, royaltyRecipient, royaltyBps)
        );
        address proxyAddress = TWFactory(_factory).deployProxy(bytes32(bytes(_contractType)), _initializer);
        emit ProxyAddress(proxyAddress);
        pack = Pack(payable(proxyAddress));
        tokenOwner = Wallet(address(this));

        packContents.push(
            ITokenBundle.Token({
                assetContract: NATIVE_TOKEN,
                tokenType: ITokenBundle.TokenType.ERC20,
                tokenId: 0,
                totalAmount: 20 ether
            })
        );
        numOfRewardUnits.push(20);
        pack.createPack{ value: 20 ether }(packContents, numOfRewardUnits, packUri, 0, 1, _recipient);
    }
}
