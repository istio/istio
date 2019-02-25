#!/bin/bash
WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# Exit immediately for non zero status
set -e
# # Check unset variables
set -u
# # Print commands
set -x
export TIME_TO_RUN_PERF_TESTS=${TIME_TO_RUN_PERF_TESTS:-1200}

kubectl -n istio-system port-forward $(kubectl get pod --namespace istio-system --selector="app=prometheus" --output jsonpath='{.items[0].metadata.name}') 8060:9090 & > /tmp/forward

sleep 5s

source "${ROOT}/prow/lib.sh"
perf_metrics="${ARTIFACTS_DIR}/perf_metrics.txt"

pushd ${GOPATH}/src/istio.io/tools/perf/twoPodTest/runner

python prom.py http://localhost:8060 ${TIME_TO_RUN_PERF_TESTS} > ${perf_metrics}

gsutil -q cp ${perf_metrics} "gs://$CB_GCS_BUILD_PATH/perf_metrics.txt"

popd

