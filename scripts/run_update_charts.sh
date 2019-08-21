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

ISTIO_DIR="${GOPATH}/src/istio.io"
OPERATOR_DIR="${ISTIO_DIR}/operator"
INSTALLER_DIR="${ISTIO_DIR}/installer"
OUT_DIR="${OPERATOR_DIR}/data/charts"
SHA=`cat ${OPERATOR_DIR}/installer.sha`

if [[ "$#" -eq 1 ]]; then
    SHA="${1}"
fi

if [[ ! -d "${INSTALLER_DIR}" ]]; then
    git clone https://github.com/istio/installer.git "${INSTALLER_DIR}"
fi

pushd .
cd "${INSTALLER_DIR}"
git checkout "${SHA}"
popd

# create charts directory if it doesn't exist.
mkdir -p "${OUT_DIR}"

for c in crds gateways istio-cni istiocoredns istio-telemetry istio-control istio-policy security
do
    cp -Rf "${INSTALLER_DIR}/${c}" "${OUT_DIR}"
done

