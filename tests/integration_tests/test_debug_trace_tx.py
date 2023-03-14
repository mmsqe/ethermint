from .utils import (
    contract_path,
    deploy_contract,
    derive_new_account,
    send_transaction,
    sign_transaction,
)


def test_trace_blk_tx(ethermint):
    w3 = ethermint.w3
    cli = ethermint.cosmos_cli()
    acc = derive_new_account(3)
    sender = acc.address
    # fund new sender
    fund = 2013208966011064811 # 2100000000000000000
    tx = {"to": sender, "value": fund, "gasPrice": w3.eth.gas_price}
    send_transaction(w3, tx)
    assert w3.eth.get_balance(sender, "latest") == fund
    destroyee, res0 = deploy_contract(
        w3,
        contract_path("Destroyee", "TestSuicide.sol"),
        (),
        acc.key,
    )
    destroyer, res1 = deploy_contract(
        w3,
        contract_path("Destroyer", "TestSuicide.sol"),
        (),
        acc.key,
    )
    print("destroyee", res0["gasUsed"] * res0["effectiveGasPrice"]) # 4477574024212883
    print("destroyer", res1["gasUsed"] * res1["effectiveGasPrice"]) # 5678754071245915
    assert len(w3.eth.get_code(destroyee.address)) > 0
    assert len(w3.eth.get_code(destroyer.address)) > 0
    tx = destroyer.functions.check_codesize_after_suicide(
        destroyee.address
    ).build_transaction()
    signed = sign_transaction(w3, tx, acc.key)
    # blk = wait_for_new_blocks(cli, 1, sleep=0.1)
    nonce = w3.eth.get_transaction_count(sender)
    txhashes = []
    txhash = w3.eth.send_raw_transaction(signed.rawTransaction)
    print("txhash0: ", txhash.hex()) # 1300698594349992
    txhashes.append(txhash)
    # 9240250433658706+857256082179000*2+1000000000000000000*2+89045237401983294
    # 9240250433658706+1300698594349992+2089459050971991302
    # 4477574024212883+5678754071245915+1337955352544013+2088505716551997189
    total = 2
    for n in range(total):
        tx = {
            "to": "0x2956c404227Cc544Ea6c3f4a36702D0FD73d20A2",
            "value": 1000000000000000000,
            "gas": 21000,
            "maxFeePerGas": 6556868066901,
            "maxPriorityFeePerGas": 1500000000,
            "nonce": nonce + n + 1,
        }
        signed = sign_transaction(w3, tx, acc.key)
        txhash = w3.eth.send_raw_transaction(signed.rawTransaction)
        print("txhash1: ", txhash.hex())
        txhashes.append(txhash)

    # url = f"http://127.0.0.1:{ports.evmrpc_port(ethermint.base_port(0))}"
    # params = {
    #     "method": "debug_traceTransaction",
    #     "params": [""],
    #     "id": 1,
    #     "jsonrpc": "2.0",
    # }
    # rsp = requests.post(url, json=params)
    # assert rsp.status_code == 200
    # print("rsp:", rsp.json()["result"])

    for txhash in txhashes:
        res = w3.eth.wait_for_transaction_receipt(txhash)
        assert res.status == 1
        print("res: ", res)
        print("gas1", res["gasUsed"] * res["effectiveGasPrice"])

    print("balance", w3.eth.get_balance(sender, "latest"))
