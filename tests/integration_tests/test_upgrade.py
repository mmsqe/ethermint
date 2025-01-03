import configparser
import hashlib
import json
import re
import subprocess
from pathlib import Path

import pytest
from pystarport import ports
from pystarport.cluster import SUPERVISOR_CONFIG_FILE

from .network import Ethermint, setup_custom_ethermint
from .utils import (
    ADDRS,
    CONTRACTS,
    approve_proposal,
    deploy_contract,
    eth_to_bech32,
    send_transaction,
    submit_gov_proposal,
    wait_for_block,
    wait_for_port,
)

pytestmark = pytest.mark.upgrade


def init_cosmovisor(home):
    """
    build and setup cosmovisor directory structure in each node's home directory
    """
    cosmovisor = home / "cosmovisor"
    cosmovisor.mkdir()
    (cosmovisor / "upgrades").symlink_to("../../../upgrades")
    (cosmovisor / "genesis").symlink_to("./upgrades/genesis")


def post_init(path, base_port, config):
    """
    prepare cosmovisor for each node
    """
    chain_id = "ethermint_9000-1"
    cfg = json.loads((path / chain_id / "config.json").read_text())
    for i, _ in enumerate(cfg["validators"]):
        home = path / chain_id / f"node{i}"
        init_cosmovisor(home)

    # patch supervisord ini config
    ini_path = path / chain_id / SUPERVISOR_CONFIG_FILE
    ini = configparser.RawConfigParser()
    ini.read(ini_path)
    reg = re.compile(rf"^program:{chain_id}-node(\d+)")
    for section in ini.sections():
        m = reg.match(section)
        if m:
            i = m.group(1)
            ini[section].update(
                {
                    "command": f"cosmovisor start --home %(here)s/node{i}",
                    "environment": (
                        f"DAEMON_NAME=ethermintd,DAEMON_HOME=%(here)s/node{i}"
                    ),
                }
            )
    with ini_path.open("w") as fp:
        ini.write(fp)


@pytest.fixture(scope="module")
def custom_ethermint(tmp_path_factory):
    path = tmp_path_factory.mktemp("upgrade")
    cmd = [
        "nix-build",
        Path(__file__).parent / "configs/upgrade-test-package.nix",
        "-o",
        path / "upgrades",
    ]
    print(*cmd)
    subprocess.run(cmd, check=True)
    # init with genesis binary
    yield from setup_custom_ethermint(
        path,
        26100,
        Path(__file__).parent / "configs/cosmovisor.jsonnet",
        post_init=post_init,
        chain_binary=str(path / "upgrades/genesis/bin/ethermintd"),
    )


def test_cosmovisor_upgrade(custom_ethermint: Ethermint, tmp_path):
    """
    - propose an upgrade and pass it
    - wait for it to happen
    - it should work transparently
    - check that queries on legacy blocks still works after upgrade.
    """
    cli = custom_ethermint.cosmos_cli()
    w3 = custom_ethermint.w3
    contract, _ = deploy_contract(w3, CONTRACTS["TestERC20A"])
    old_height = w3.eth.block_number
    old_balance = w3.eth.get_balance(ADDRS["validator"], block_identifier=old_height)
    old_base_fee = w3.eth.get_block(old_height).baseFeePerGas
    old_erc20_balance = contract.caller.balanceOf(ADDRS["validator"])
    print("old values", old_height, old_balance, old_base_fee)

    target_height = w3.eth.block_number + 10
    print("upgrade height", target_height)

    plan_name = "sdk50"
    rsp = cli.gov_propose_legacy(
        "community",
        "software-upgrade",
        {
            "name": plan_name,
            "title": "upgrade test",
            "description": "ditto",
            "upgrade-height": target_height,
            "deposit": "10000aphoton",
        },
    )
    assert rsp["code"] == 0, rsp["raw_log"]
    approve_proposal(custom_ethermint, rsp)

    # update cli chain binary
    custom_ethermint.chain_binary = (
        Path(custom_ethermint.chain_binary).parent.parent.parent
        / f"{plan_name}/bin/ethermintd"
    )
    cli = custom_ethermint.cosmos_cli()

    # block should pass the target height
    wait_for_block(cli, target_height + 1, timeout=480)
    wait_for_port(ports.rpc_port(custom_ethermint.base_port(0)))

    # test migrate keystore
    cli.migrate_keystore()

    # check basic tx works after upgrade
    wait_for_port(ports.evmrpc_port(custom_ethermint.base_port(0)))

    receipt = send_transaction(
        w3,
        {
            "to": ADDRS["community"],
            "value": 1000,
            "maxFeePerGas": 1000000000000,
            "maxPriorityFeePerGas": 10000,
        },
    )
    assert receipt.status == 1

    # check json-rpc query on older blocks works
    assert old_balance == w3.eth.get_balance(
        ADDRS["validator"], block_identifier=old_height
    )
    assert old_base_fee == w3.eth.get_block(old_height).baseFeePerGas

    # check eth_call on older blocks works
    assert old_erc20_balance == contract.caller(
        block_identifier=target_height - 2
    ).balanceOf(ADDRS["validator"])
    p = json.loads(
        cli.raw(
            "query",
            "ibc",
            "client",
            "params",
            home=cli.data_dir,
        )
    )
    assert p == {"allowed_clients": ["06-solomachine", "07-tendermint", "09-localhost"]}

    p = cli.get_params("evm")["params"]
    header_hash_num = "20"
    p["header_hash_num"] = header_hash_num
    # governance module account as signer
    data = hashlib.sha256("gov".encode()).digest()[:20]
    authority = eth_to_bech32(data)
    submit_gov_proposal(
        custom_ethermint,
        tmp_path,
        messages=[
            {
                "@type": "/ethermint.evm.v1.MsgUpdateParams",
                "authority": authority,
                "params": p,
            }
        ],
    )
    p = cli.get_params("evm")["params"]
    assert p["header_hash_num"] == header_hash_num, p
    contract, _ = deploy_contract(w3, CONTRACTS["TestBlockTxProperties"])
    for h in [target_height - 1, target_height, target_height + 1]:
        res = contract.caller.getBlockHash(h).hex()
        blk = w3.eth.get_block(h)
        assert f"0x{res}" == blk.hash.hex(), res

    height = w3.eth.block_number
    for h in [
        height - int(header_hash_num) - 1,  # num64 < lower
        height + 100,  # num64 >= upper
    ]:
        res = contract.caller.getBlockHash(h).hex()
        assert f"0x{res}" == "0x" + "0" * 64, res


    target_height = w3.eth.block_number + 10
    print("upgrade height", target_height)

    plan_name = "sdk52"
    rsp = cli.software_upgrade(
        "community",
        {
            "name": plan_name,
            "title": "upgrade test",
            "note": "ditto",
            "upgrade-height": target_height,
            "summary": "summary",
            "deposit": "10000aphoton",
        },
    )
    assert rsp["code"] == 0, rsp["raw_log"]
    approve_proposal(custom_ethermint, rsp)

    # update cli chain binary
    custom_ethermint.chain_binary = (
        Path(custom_ethermint.chain_binary).parent.parent.parent
        / f"{plan_name}/bin/ethermintd"
    )
    cli = custom_ethermint.cosmos_cli()

    # block should pass the target height
    wait_for_block(cli, target_height + 1, timeout=480)
    wait_for_port(ports.rpc_port(custom_ethermint.base_port(0)))
    
    # check basic tx works
    receipt = send_transaction(
        custom_ethermint.w3,
        {
            "to": ADDRS["community"],
            "value": 1000,
            "maxFeePerGas": 10000000000000,
            "maxPriorityFeePerGas": 10000,
        },
    )
    assert receipt.status == 1