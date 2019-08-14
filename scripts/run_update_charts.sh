#!/bin/bash

# Copyright 2019 Istio Authors
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

# No unset vars, print commands as they're executed, and exit on any non-zero
# return code
set -u
set -x
set -e

if [[ "$#" -ne 2 ]]; then
    echo "Usage: run_update_charts.sh <sha or branch>"
fi

SHA="${1}"

INSTALLER_DIR=${GOPATH}/src/istio.io/installer
OUT_DIR=${GOPATH}/src/istio.io/operator/data/charts

if [[ ! -d "${INSTALLER_DIR}" ]]; then
    git clone https://github.com/istio/installer.git "${INSTALLER_DIR}"
fi

pushd .
cd "${INSTALLER_DIR}"
git checkout "${SHA}"
popd

for c in crds gateways istio-cni istiocoredns istio-telemetry istio-control istio-policy security
do
    cp -Rf "${INSTALLER_DIR}/${c}" "${OUT_DIR}"
done

