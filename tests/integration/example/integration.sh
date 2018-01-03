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

function process_result() {
    if [[ $1 -eq 0 ]]; then
        SUMMARY+="\nPASSED: $2 "
    else
        SUMMARY+="\nFAILED: $2 "
        ((FAILURE_COUNT++))
    fi
}

# Build mixer binary
bazel build //mixer/cmd/mixs:mixs
MIXER_BINARY=$(pwd)/bazel-bin/mixer/cmd/mixs/mixs

# Get fortio
go get -u istio.io/fortio

# Download Proxy
cd ..
ls proxy || git clone https://github.com/istio/proxy
cd proxy
#git pull
bazel build //src/envoy/mixer:envoy
ENVOY_BINARY=$(pwd)/src/envoy/mixer/start_envoy
cd ../istio

# Run Tests
TESTSPATH='tests/integration/example/tests'
TESTS_TARGETS=($(bazel query "tests(//${TESTSPATH}/...)")) || error_exit 'Could not find tests targets'
TOTAL_FAILURE=0
SUMMARY='Tests Summary'

TESTARG=(-envoy_binary ${ENVOY_BINARY} -mixer_binary ${MIXER_BINARY} -fortio_binary fortio)

for T in ${TESTS_TARGETS[@]}; do
    echo "Running ${T}"
    bazel run ${T} -- ${TESTARG[@]}
    process_result $? ${T}
done

printf "${SUMMARY}\n"
exit ${FAILURE_COUNT}
