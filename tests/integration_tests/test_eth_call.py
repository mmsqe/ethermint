import itertools
import json
from concurrent.futures import ThreadPoolExecutor, as_completed

from web3 import Web3

from .utils import CONTRACTS, deploy_contract, derive_new_account, send_transaction


def fund_acc(w3, acc):
    fund = 3000000000000000000
    addr = acc.address
    if w3.eth.get_balance(addr, "latest") == 0:
        tx = {"to": addr, "value": fund, "gasPrice": w3.eth.gas_price}
        send_transaction(w3, tx)
        assert w3.eth.get_balance(addr, "latest") == fund


def get_selector(address="c05A754131288197F958CF36C6C807FAF5Ec12D6"):
    func = "balanceOf(address)"
    func_hash = Web3.keccak(text=func).hex()[2:10]
    return f"0x{func_hash}{address.lower().rjust(64, '0')}"


def test_eth_call(ethermint, geth):
    method = "eth_call"
    acc = derive_new_account(6)

    def process(w3):
        fund_acc(w3, acc)
        erc20, receipt = deploy_contract(w3, CONTRACTS["TestERC20A"], key=acc.key)
        selector = get_selector()
        call = w3.provider.make_request
        blks = ["latest", hex(receipt["blockNumber"] - 1)]
        print("mm-blks", blks)
        with ThreadPoolExecutor(len(blks)) as exec:
            params = [[{"to": erc20.address, "data": selector}, blk] for blk in blks]
            exec_map = exec.map(call, itertools.repeat(method), params)
            res = [json.dumps(resp["result"], sort_keys=True) for resp in exec_map]
        return res

    providers = [ethermint.w3, geth.w3]
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1], res
