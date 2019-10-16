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

set -e
set -u
set -x

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")
OUT=${OUT:-/tmp/operator-migrate-out}
rm -Rf "${OUT}"
mkdir -p "${OUT}"

CHARTS_DIR=$(mktemp -d)

git clone https://github.com/istio/installer.git "${CHARTS_DIR}"

SHA="$(cat "${ROOT}"/installer.sha)"

pushd .
cd "${CHARTS_DIR}"
git checkout "${SHA}"
# exclude from migrate target
rm -r ./test ./kustomize
popd

cd "${ROOT}"
export GO111MODULE=on

# this command would generate a migrated profile in IstioControlPlane CR format
# and the diff with current profile as reference to update.
function run_migrate_command() {
    local profile="${1}"
    local out_profile_path="${OUT}/profiles/${profile}"
    mkdir -p "${OUT}/profiles"
    go run ./cmd/mesh.go manifest migrate "${CHARTS_DIR}" > "${out_profile_path}"
    go run ./cmd/mesh.go manifest diff "${out_profile_path}" "${ROOT}/data/profiles/${profile}.yaml"
}

# check the default profile.
run_migrate_command "default"
