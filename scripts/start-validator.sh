#!/bin/sh
set -e

DATA=$1
if [ -z $DATA ]; then
    echo "No data directory supplied"
    exit 1
fi
shift

bash ../../scripts/prysm.sh validator \
--datadir $DATA \
--accept-terms-of-use \
--interop-num-validators=1 \
--chain-config-file=config.yaml