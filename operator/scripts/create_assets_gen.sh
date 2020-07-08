#!/bin/bash

# Copyright Istio Authors
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

"${SCRIPTPATH}"/create_release_charts.sh -o "${OUT_DIR}"

cd "${OUT_DIR}"
go-bindata --nocompress --nometadata --pkg vfs -o "${OPERATOR_DIR}/pkg/vfs/assets.gen.go" ./...

rm -Rf "${OUT_DIR}"
