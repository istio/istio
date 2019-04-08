#!/bin/bash
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
CMD=""
DELETE=""
source "${ROOT}/prow/lib.sh"

TIME_TO_RUN_PERF_TESTS=${TIME_TO_RUN_PERF_TESTS:-1200}

pushd ${GOPATH}/src/istio.io/tools/perf/servicegraph
  WD=${GOPATH}/src/istio.io/tools/perf/servicegraph
  source common.sh
  start_servicegraphs "1" "0"
popd
# Run the test for some time
echo "Run the test for ${TIME_TO_RUN_PERF_TESTS}"

kubectl -n istio-system port-forward $(kubectl get pod --namespace istio-system --selector="app=prometheus" --output jsonpath='{.items[0].metadata.name}') 8060:9090 > /tmp/forward &

sleep 5s

OUTPUT_PATH=${OUTPUT_PATH:-"/tmp/output"}

mkdir -p "${OUTPUT_PATH}"

perf_metrics="${OUTPUT_PATH}/perf_metrics.txt"
rm "${perf_metrics}" || true

pushd ${GOPATH}/src/istio.io/tools/perf/twoPodTest/runner
  count=$(expr $TIME_TO_RUN_PERF_TESTS / 60)
  echo "Get metric $count time(s)."
  for i in `seq 1 $count`;
  do
    sleep 1m
    python prom.py http://localhost:8060 60 --no-aggregate >> ${perf_metrics}
  done

#  gsutil -q cp ${perf_metrics} "gs://$CB_GCS_BUILD_PATH/perf_metrics.txt"

popd

exec "$1"

