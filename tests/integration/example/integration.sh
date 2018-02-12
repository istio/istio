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

# If GOPATH is not set by the env, set it to a sane value
GOPATH ?= $(shell cd ../../..; pwd)
export GOPATH

# Build mixer binary
make mixs
MIXER_BINARY=${GOPATH}/bin/mixs

# Download Proxy
source istio.VERSION
cd ..
ls proxy || git clone https://github.com/istio/proxy
cd proxy
git pull

PROXY_TAR="envoy-debug-${PROXY_TAG}.tar.gz"
rm -rf usr ${PROXY_TAR}
wget "https://storage.googleapis.com/istio-build/proxy/${PROXY_TAR}"
tar xvzf "${PROXY_TAR}"

ENVOY_BINARY=$(pwd)/usr/local/bin/envoy
START_ENVOY=$(pwd)/src/envoy/mixer/start_envoy
cd ../istio

# Install Fortio
cd vendor/istio.io/fortio
make install
cd ../../..

# Run Tests
TESTSPATH='tests/integration/example/tests'
TOTAL_FAILURE=0
SUMMARY='Tests Summary'

TESTARG=(-envoy_binary ${ENVOY_BINARY} -envoy_start_script ${START_ENVOY} -mixer_binary ${MIXER_BINARY} -fortio_binary fortio)

go test -v ./tests/integration/example/tests/sample1 ${TESTARG[@]}
process_result $? sample1

go test -v ./tests/integration/example/tests/sample2 ${TESTARG[@]}
process_result $? sample2

printf "${SUMMARY}\n"
exit ${FAILURE_COUNT}
