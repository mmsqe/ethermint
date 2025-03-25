// SPDX-License-Identifier: MIT

pragma solidity ^0.8.0;

import { Pack } from "@thirdweb-dev/contracts/prebuilts/pack/Pack.sol";
import { ITokenBundle } from "@thirdweb-dev/contracts/extension/interface/ITokenBundle.sol";

import { Wallet } from "./utils/Wallet.sol";
import "./mocks/MockERC20.sol";
import "./mocks/MockERC721.sol";
import "./mocks/MockERC1155.sol";
import "./mocks/WETH9.sol";

import { Forwarder } from "@thirdweb-dev/contracts/infra/forwarder/Forwarder.sol";
import { TWRegistry } from "@thirdweb-dev/contracts/infra/TWRegistry.sol";
import { TWFactory } from "@thirdweb-dev/contracts/infra/TWFactory.sol";


contract TestPack is Wallet {
    Pack internal pack;

    Wallet internal tokenOwner;
    string internal packUri;
    ITokenBundle.Token[] internal packContents;
    uint256[] internal numOfRewardUnits;

    string public constant NAME = "NAME";
    string public constant SYMBOL = "SYMBOL";
    string public constant CONTRACT_URI = "CONTRACT_URI";

    MockERC20 public erc20;
    MockERC721 public erc721;
    MockERC1155 public erc1155;
    WETH9 public weth;

    address public forwarder;
    address public registry;
    address public factory;
    address public fee;

    address public royaltyRecipient = address(0x30001);
    uint128 public royaltyBps = 500; // 5%
    event ProxyAddress(address indexed proxyAddress);

    function setUp(
        address _erc20,
        address _erc721,
        address _erc1155,
        address _weth,
        address _forwarder,
        address _registry,
        address _factory,
        address recipient
    ) public {
        erc20 = MockERC20(_erc20);
        erc721 = MockERC721(_erc721);
        erc1155 = MockERC1155(_erc1155);
        weth = WETH9(payable(_weth));
        forwarder = _forwarder;
        registry = _registry;
        factory = _factory;

        string memory _contractType = "Pack";
        bytes memory _initializer = abi.encodeCall(
            Pack.initialize,
            (address(this), NAME, SYMBOL, CONTRACT_URI, forwarders(), royaltyRecipient, royaltyBps)
        );
        address proxyAddress = TWFactory(factory).deployProxy(bytes32(bytes(_contractType)), _initializer);
        emit ProxyAddress(proxyAddress);

        pack = Pack(payable(proxyAddress));

        tokenOwner = Wallet(address(this));
        packUri = "ipfs://";

        packContents.push(
            ITokenBundle.Token({
                assetContract: address(erc721),
                tokenType: ITokenBundle.TokenType.ERC721,
                tokenId: 0,
                totalAmount: 1
            })
        );
        numOfRewardUnits.push(1);

        packContents.push(
            ITokenBundle.Token({
                assetContract: address(erc1155),
                tokenType: ITokenBundle.TokenType.ERC1155,
                tokenId: 0,
                totalAmount: 100
            })
        );
        numOfRewardUnits.push(20);

        packContents.push(
            ITokenBundle.Token({
                assetContract: address(erc20),
                tokenType: ITokenBundle.TokenType.ERC20,
                tokenId: 0,
                totalAmount: 1000 ether
            })
        );
        numOfRewardUnits.push(50);

        packContents.push(
            ITokenBundle.Token({
                assetContract: address(erc721),
                tokenType: ITokenBundle.TokenType.ERC721,
                tokenId: 1,
                totalAmount: 1
            })
        );
        numOfRewardUnits.push(1);

        packContents.push(
            ITokenBundle.Token({
                assetContract: address(erc721),
                tokenType: ITokenBundle.TokenType.ERC721,
                tokenId: 2,
                totalAmount: 1
            })
        );
        numOfRewardUnits.push(1);

        packContents.push(
            ITokenBundle.Token({
                assetContract: address(erc20),
                tokenType: ITokenBundle.TokenType.ERC20,
                tokenId: 0,
                totalAmount: 1000 ether
            })
        );
        numOfRewardUnits.push(100);

        packContents.push(
            ITokenBundle.Token({
                assetContract: address(erc721),
                tokenType: ITokenBundle.TokenType.ERC721,
                tokenId: 3,
                totalAmount: 1
            })
        );
        numOfRewardUnits.push(1);

        packContents.push(
            ITokenBundle.Token({
                assetContract: address(erc721),
                tokenType: ITokenBundle.TokenType.ERC721,
                tokenId: 4,
                totalAmount: 1
            })
        );
        numOfRewardUnits.push(1);

        packContents.push(
            ITokenBundle.Token({
                assetContract: address(erc721),
                tokenType: ITokenBundle.TokenType.ERC721,
                tokenId: 5,
                totalAmount: 1
            })
        );
        numOfRewardUnits.push(1);

        packContents.push(
            ITokenBundle.Token({
                assetContract: address(erc1155),
                tokenType: ITokenBundle.TokenType.ERC1155,
                tokenId: 1,
                totalAmount: 500
            })
        );
        numOfRewardUnits.push(50);

        erc20.mint(address(tokenOwner), 2000 ether);
        erc721.mint(address(tokenOwner), 6);
        erc1155.mint(address(tokenOwner), 0, 100);
        erc1155.mint(address(tokenOwner), 1, 500);

        tokenOwner.setAllowanceERC20(address(erc20), address(pack), type(uint256).max);
        tokenOwner.setApprovalForAllERC721(address(erc721), address(pack), true);
        tokenOwner.setApprovalForAllERC1155(address(erc1155), address(pack), true);

        // pack.grantRole(keccak256("MINTER_ROLE"), address(tokenOwner));
        (uint256 packId, uint256 totalSupply) = pack.createPack(packContents, numOfRewardUnits, packUri, 0, 2, recipient);
    }

    function forwarders() public view returns (address[] memory) {
        address[] memory _forwarders = new address[](1);
        _forwarders[0] = forwarder;
        return _forwarders;
    }
}
