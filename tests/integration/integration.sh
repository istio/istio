#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

# Print commands
set -x

cleanup () {
  if [ ! -z "${TEMP_DIR}" ] && [ -d "${TEMP_DIR}" ]; then
    rm -r ${TEMP_DIR}
  fi
}
#trap cleanup EXIT

function process_result() {
    if [[ $1 -eq 0 ]]; then
        SUMMARY+="\nPASSED: $2 "
    else
        SUMMARY+="\nFAILED: $2 "
        ((FAILURE_COUNT++))
    fi
}

#cleanup
TEMP_DIR=$(pwd)/integration_tmp
mkdir ${TEMP_DIR}


# Build mixer binary
bazel build -c opt mixer/cmd/server:mixs

# Get fortio
go get -u istio.io/fortio

# Download Proxy
ISTIO_PROXY_BUCKET="ad3f963c6a197b8ad36c9f9428986c7fe84d20ca"
ENVOY_URL="https://storage.googleapis.com/istio-build/proxy/envoy-debug-${ISTIO_PROXY_BUCKET}.tar.gz"
wget -O ${TEMP_DIR}/envoy_tar ${ENVOY_URL}
tar xvfz ${TEMP_DIR}/envoy_tar
ENVOY_BINARY=${TEMP_DIR}/usr/local/bin/envoy

# Run Tests
TESTSPATH='tests/integration/tests'
TESTS_TARGETS=($(bazel query "tests(//${TESTSPATH}/...)")) || error_exit 'Could not find tests targets'
TOTAL_FAILURE=0
SUMMARY='Tests Summary'

for T in ${TESTS_TARGETS[@]}; do
    print_block "Running ${T}"
    bazel run ${T}
    process_result $? ${T}
done