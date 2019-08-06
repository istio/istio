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

function update_version() {
    local REALASE_DIR="$1"
    local DOCKER_HUB="$2"
    local DOCKER_TAG="$3"
    # Update version string in profiles.
    sed -i "s|hub: gcr.io/istio-release|hub: ${DOCKER_HUB}|g" ${REALASE_DIR}/profiles/*.yaml
    sed -i "s|tag: .*-latest-daily|tag: ${DOCKER_TAG}|g"      ${REALASE_DIR}/profiles/*.yaml
    # Update version string in global.yaml.
    sed -i "s|hub: gcr.io/istio-release|hub: ${DOCKER_HUB}|g" ${REALASE_DIR}/charts/global.yaml
    sed -i "s|tag: .*-latest-daily|tag: ${DOCKER_TAG}|g"      ${REALASE_DIR}/charts/global.yaml
}
