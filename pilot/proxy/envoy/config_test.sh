#!/bin/bash
# Check that envoy can parse golden configuration artifacts
# In the future, we can run functional tests, but all configs require iptables rules to be effective.

set -o errexit
set -o nounset
set -o pipefail

ENVOY="proxy/envoy/envoy -l trace --service-cluster istio-proxy --service-node 10.0.0.1"
CONFIG_FILES="envoy-v0 envoy-fault ingress-envoy egress-envoy"

# TODO the test skips over SSL configuration files since referenced cert files are not in the right places

for f in ${CONFIG_FILES}; do
  echo "##############################"
  echo "# Testing $f..."
  echo "##############################"
  ${ENVOY} -c proxy/envoy/testdata/$f.json.golden &
  PID=$!
  sleep 1
  kill -KILL $PID
done
