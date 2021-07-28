#!/bin/bash

# Copyright 2020 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Update the Proxy SHA in istio.deps with the first argument
# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR=$(dirname "${SCRIPTPATH}")
cd "${ROOTDIR}"

# Wait for the proxy to become available
ISTIO_ENVOY_VERSION=${ISTIO_ENVOY_VERSION:-$1}
ISTIO_ENVOY_LINUX_VERSION=${ISTIO_ENVOY_LINUX_VERSION:-${ISTIO_ENVOY_VERSION}}
ISTIO_ENVOY_BASE_URL=${ISTIO_ENVOY_BASE_URL:-https://storage.googleapis.com/istio-build/proxy}
ISTIO_ENVOY_RELEASE_URL=${ISTIO_ENVOY_RELEASE_URL:-${ISTIO_ENVOY_BASE_URL}/envoy-alpha-${ISTIO_ENVOY_LINUX_VERSION}.tar.gz}
SLEEP_TIME=60

printf "Verifying %s is available\n" "$ISTIO_ENVOY_RELEASE_URL"
until curl --output /dev/null --silent --head --fail "$ISTIO_ENVOY_RELEASE_URL"; do
    printf '.'
    sleep $SLEEP_TIME
done
printf '\n'

plugin=metadata_exchange
WASM_URL=${ISTIO_ENVOY_BASE_URL}/${plugin}-${ISTIO_ENVOY_VERSION}.wasm
printf "Verifying %s is available\n" "$WASM_URL"
until curl --output /dev/null --silent --head --fail "$WASM_URL"; do
    printf '.'
    sleep $SLEEP_TIME
done
printf '\n'

# Update the dependency in istio.deps
sed -i '/PROXY_REPO_SHA/,/lastStableSHA/ { s/"lastStableSHA":.*/"lastStableSHA": "'"$1"'"/  }' istio.deps
