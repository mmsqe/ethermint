from contextlib import contextmanager

import pytest

from .network import setup_beacon, setup_ethermint, setup_geth, setup_validator


def pytest_configure(config):
    config.addinivalue_line("markers", "unmarked: fallback mark for unmarked tests")
    config.addinivalue_line("markers", "upgrade: upgrade tests")
    config.addinivalue_line("markers", "filter: filter tests")


def pytest_collection_modifyitems(items, config):
    for item in items:
        if not any(item.iter_markers()):
            item.add_marker("unmarked")


@pytest.fixture(scope="session")
def ethermint(tmp_path_factory):
    path = tmp_path_factory.mktemp("ethermint")
    yield from setup_ethermint(path, 26650)


@contextmanager
def setup_all(path, base_port):
    geth_gen = setup_geth(path, base_port)
    beacon_gen = setup_beacon(path, base_port)
    validator_gen = setup_validator(path, base_port)
    geth_instance = next(geth_gen)
    next(beacon_gen)
    next(validator_gen)

    try:
        yield geth_instance
    finally:
        pass


@pytest.fixture(scope="session")
def geth(tmp_path_factory):
    path = tmp_path_factory.mktemp("geth")
    with setup_all(path, 8545) as geth_instance:
        yield geth_instance


@pytest.fixture(scope="session", params=["ethermint", "ethermint-ws"])
def ethermint_rpc_ws(request, ethermint):
    """
    run on both ethermint and ethermint websocket
    """
    provider = request.param
    if provider == "ethermint":
        yield ethermint
    elif provider == "ethermint-ws":
        ethermint_ws = ethermint.copy()
        ethermint_ws.use_websocket()
        yield ethermint_ws
    else:
        raise NotImplementedError
