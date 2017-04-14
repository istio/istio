#!/bin/bash
# Check that envoy can parse golden configuration artifacts
# In the future, we can run functional tests, but all configs require iptables rules to be effective.

# Usage: config_test.sh <envoy-binary> <envoy.json> <base-id>

set -o errexit
set -o nounset
set -o pipefail

ENVOY="$1 -l trace --service-cluster istio-proxy --service-node 10.0.0.1"
CONFIG_FILE=$2
BASE_ID=$3

${ENVOY} --base-id ${BASE_ID} -c ${CONFIG_FILE} &
PID=$!
sleep 0.200
kill -KILL $PID
