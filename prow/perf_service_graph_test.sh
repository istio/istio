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
########???????????????????
CMD=""
DELETE=""


export TIME_TO_RUN_PERF_TESTS=${TIME_TO_RUN_PERF_TESTS:-1200}


pushd ${GOPATH}/src/istio.io/tools/perf/servicegraph
  WD=${GOPATH}/src/istio.io/tools/perf/servicegraph
  source common.sh
  start_servicegraphs "1" "0"
popd
# Run the test for some time
echo "Run the test for ${TIME_TO_RUN_PERF_TESTS}"
sleep ${TIME_TO_RUN_PERF_TESTS}

# Run the test script in istio/istio
exec "$1"

