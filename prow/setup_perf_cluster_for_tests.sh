#!/bin/bash
WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

# Check https://github.com/istio/test-infra/blob/master/boskos/configs.yaml
# for existing resources types
export RESOURCE_TYPE="${RESOURCE_TYPE:-gke-perf-preset}"
export OWNER="${OWNER:-perf-tests}"
export PILOT_CLUSTER="${PILOT_CLUSTER:-}"
export USE_MASON_RESOURCE="${USE_MASON_RESOURCE:-True}"
export CLEAN_CLUSTERS="${CLEAN_CLUSTERS:-True}"
# This is config for postsubmit cluster under istio/tools/perf/istio.
export VALUES="${VALUES:-values-istio-postsubmit.yaml}"
export DNS_DOMAIN="fake_dns.org"

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
setup_e2e_cluster

echo "Get istio release: $TAG"
pushd "${GOPATH}/src/istio.io/tools/perf/istio-install"
  ./setup_istio.sh "${TAG}"
popd

# Run the test script in istio/istio
exec "$1"
