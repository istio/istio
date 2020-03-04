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

OUT_DIR=$(mktemp -d -t istio-charts.XXXXXXXXXX) || { echo "Failed to create temp file"; exit 1; }

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR="${SCRIPTPATH}/../.."

OPERATOR_DIR="${ROOTDIR}/operator"
INSTALLER_DIR="${ROOTDIR}/manifests"

cp -Rf "${OPERATOR_DIR}"/data/* "${OUT_DIR}/."

mkdir -p "${OUT_DIR}/charts"

for c in base gateways istio-cni istiocoredns istio-telemetry istio-control istio-policy security
do
    cp -Rf "${INSTALLER_DIR}/${c}" "${OUT_DIR}/charts"
done

cd "${OUT_DIR}"
go-bindata --nocompress --nometadata --pkg vfs -o "${OPERATOR_DIR}/pkg/vfs/assets.gen.go" ./...

rm -Rf "${OUT_DIR}"
