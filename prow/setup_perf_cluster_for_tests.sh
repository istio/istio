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
export DNS_DOMAIN="fake-dns.org"
export CMD=""
export DELETE=""

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
setup_e2e_cluster
helm init --client-only

echo "Get istio release: $TAG"
pushd "${GOPATH}/src/istio.io/tools/perf/istio-install"
  ./setup_istio.sh "${TAG}"
popd

echo "Run perf test."

TIME_TO_RUN_PERF_TESTS=${TIME_TO_RUN_PERF_TESTS:-1200}

pushd "${GOPATH}/src/istio.io/tools/perf/load"
  WD="${GOPATH}/src/istio.io/tools/perf/load"
  # shellcheck disable=SC1091
  source common.sh
  # For postsubmit test we use 1 namespace only and use 0 as prefix for namespace.
  start_servicegraphs "1" "0"
popd
# Run the test for some time
echo "Run the test for ${TIME_TO_RUN_PERF_TESTS}"
pod=$(kubectl get pod --namespace istio-system --selector="app=prometheus" --output jsonpath='{.items[0].metadata.name}')  
kubectl -n istio-system port-forward "$pod" 8060:9090 > /tmp/forward &

sleep 5s

OUTPUT_PATH=${OUTPUT_PATH:-"/tmp/output"}

mkdir -p "${OUTPUT_PATH}"

perf_metrics="${OUTPUT_PATH}/perf_metrics.txt"
rm "${perf_metrics}" || true

pushd "${GOPATH}/src/istio.io/tools/perf/benchmark/runner"
count="$((TIME_TO_RUN_PERF_TESTS / 60))"
  echo "Get metric $count time(s)."
  for i in $(seq 1 "$count");
  do
    echo "Running for $i min"
    sleep 1m
    python prom.py http://localhost:8060 60 --no-aggregate >> "${perf_metrics}"
  done

  gsutil -q cp "${perf_metrics}" "gs://$CB_GCS_BUILD_PATH/perf_metrics.txt"

popd

echo "Perf test is done."
