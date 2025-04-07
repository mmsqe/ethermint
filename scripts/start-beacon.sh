#!/bin/sh
set -e

DATA=$1
if [ -z $DATA ]; then
    echo "No data directory supplied"
    exit 1
fi
shift

bash ../../scripts/prysm.sh beacon-chain \
--datadir $DATA \
--genesis-state=genesis.ssz \
--chain-config-file=config.yaml \
--chain-id=9000 \
--min-sync-peers=0 \
--bootstrap-node= \
--interop-eth1data-votes \
--contract-deployment-block=0 \
--rpc-host=0.0.0.0 \
--grpc-gateway-host=0.0.0.0 \
--execution-endpoint=http://localhost:8551 \
--accept-terms-of-use \
--jwt-secret=jwt.hex \
--suggested-fee-recipient=0x57f96e6B86CdeFdB3d412547816a82E3E0EbF9D2 \
--minimum-peers-per-subnet=0 
