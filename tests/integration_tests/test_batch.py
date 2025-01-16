import json

from .utils import ADDRS, CONTRACTS, build_batch_tx, contract_address


def test_batch_tx(ethermint):
    "send multiple eth txs in single cosmos tx"
    w3 = ethermint.w3
    cli = ethermint.cosmos_cli()
    sender = ADDRS["validator"]
    recipient = ADDRS["community"]
    nonce = w3.eth.get_transaction_count(sender)
    info = json.loads(CONTRACTS["TestERC20A"].read_text())
    contract = w3.eth.contract(abi=info["abi"], bytecode=info["bytecode"])
    deploy_tx = contract.constructor().build_transaction(
        {"from": sender, "nonce": nonce}
    )
    contract = w3.eth.contract(address=contract_address(sender, nonce), abi=info["abi"])
    transfer_tx1 = contract.functions.transfer(recipient, 1000).build_transaction(
        {"from": sender, "nonce": nonce + 1, "gas": 200000}
    )
    transfer_tx2 = contract.functions.transfer(recipient, 1000).build_transaction(
        {"from": sender, "nonce": nonce + 2, "gas": 200000}
    )

    cosmos_tx, tx_hashes = build_batch_tx(
        w3, cli, [deploy_tx, transfer_tx1, transfer_tx2]
    )
    rsp = cli.broadcast_tx_json(cosmos_tx)
    assert rsp["code"] == 0, rsp["raw_log"]

    receipts = [w3.eth.wait_for_transaction_receipt(h) for h in tx_hashes]

    assert 2000 == contract.caller.balanceOf(recipient)

    # check logs
    assert receipts[0].contractAddress == contract.address

    assert receipts[0].transactionIndex == 0
    assert receipts[1].transactionIndex == 1
    assert receipts[2].transactionIndex == 2

    assert receipts[0].logs[0].logIndex == 0
    assert receipts[1].logs[0].logIndex == 1
    assert receipts[2].logs[0].logIndex == 2

    assert receipts[0].cumulativeGasUsed == receipts[0].gasUsed
    assert receipts[1].cumulativeGasUsed == receipts[0].gasUsed + receipts[1].gasUsed
    assert (
        receipts[2].cumulativeGasUsed
        == receipts[0].gasUsed + receipts[1].gasUsed + receipts[2].gasUsed
    )

    # check nonce
    assert w3.eth.get_transaction_count(sender) == nonce + 3

    # check traceTransaction
    rsps = [
        w3.provider.make_request("debug_traceTransaction", [h.hex()])["result"]
        for h in tx_hashes
    ]

    for rsp, receipt in zip(rsps, receipts):
        assert not rsp["failed"]
        assert receipt.gasUsed == rsp["gas"]

    # check get_transaction_by_block
    txs = [
        w3.eth.get_transaction_by_block(receipts[0].blockNumber, i) for i in range(3)
    ]
    for tx, h in zip(txs, tx_hashes):
        assert tx.hash == h

    # check getBlock
    txs = w3.eth.get_block(receipts[0].blockNumber, True).transactions
    for i in range(3):
        assert txs[i].transactionIndex == i


def test_multisig(ethermint, tmp_path):
    cli = ethermint.cosmos_cli()
    cli.make_multisig("multitest1", "signer1", "signer2")
    multi_addr = cli.address("multitest1")
    signer1 = cli.address("signer1")
    denom = "aphoton"
    amt = 2000000000000000000
    rsp = cli.transfer(signer1, multi_addr, f"{amt}{denom}")
    assert rsp["code"] == 0, rsp["raw_log"]
    assert cli.balance(multi_addr, denom=denom) == amt

    acc = cli.account(multi_addr)["account"]["value"]["base_account"]
    res = cli.account_by_num(acc["account_number"])
    assert res["account_address"] == multi_addr

    m_txt = tmp_path / "m.txt"
    p1_txt = tmp_path / "p1.txt"
    p2_txt = tmp_path / "p2.txt"
    tx_txt = tmp_path / "tx.txt"
    amt = 1
    signer2 = cli.address("signer2")
    multi_tx = cli.transfer(
        multi_addr,
        signer2,
        f"{amt}{denom}",
        generate_only=True,
    )
    json.dump(multi_tx, m_txt.open("w"))
    signature1 = cli.sign_multisig_tx(m_txt, multi_addr, "signer1")
    json.dump(signature1, p1_txt.open("w"))
    signature2 = cli.sign_multisig_tx(m_txt, multi_addr, "signer2")
    json.dump(signature2, p2_txt.open("w"))
    final_multi_tx = cli.combine_multisig_tx(
        m_txt,
        "multitest1",
        p1_txt,
        p2_txt,
    )
    json.dump(final_multi_tx, tx_txt.open("w"))
    rsp = cli.broadcast_tx(tx_txt)
    assert rsp["code"] == 0, rsp["raw_log"]
    assert (
        cli.account(multi_addr)["account"]["value"]["base_account"]["address"]
        == acc["address"]
    )


def test_textual(ethermint):
    cli = ethermint.cosmos_cli()
    rsp = cli.transfer(
        cli.address("validator"),
        cli.address("signer2"),
        "1aphoton",
        sign_mode="textual",
    )
    print("mm-rsp", rsp)
    assert rsp["code"] == 0, rsp["raw_log"]
