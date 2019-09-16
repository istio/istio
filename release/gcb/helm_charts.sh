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

# This script generates and copies helm charts within the helm tree of this repo.
# Initial setup

set -o errexit
set -o nounset
set -o pipefail
set -x

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/gcb_lib.sh
source "${SCRIPTPATH}/gcb_lib.sh"

WORK_DIR=$(mktemp -d)
HELM_DIR=$(mktemp -d)

echo WORK_DIR = "$WORK_DIR"
echo HELM_DIR = "$HELM_DIR"

# Helm setup
HELM_BUILD_DIR="/workspace/charts"

# Copy Istio release files to WORK_DIR
gsutil cp  "gs://${CB_GCS_BUILD_PATH}/istio-${CB_VERSION}-linux.tar.gz" .

create_charts "${CB_VERSION}" "${WORK_DIR}" "${HELM_DIR}" "${HELM_BUILD_DIR}" "${CB_BRANCH}" "${CB_DOCKER_HUB}"

# Copy output to GCS bucket.
gsutil -qm cp -r "${HELM_BUILD_DIR}/*" "gs://${CB_GCS_BUILD_PATH}/charts/"

# Do the cleanup.
rm -fr "${HELM_DIR}"
rm -fr "${WORK_DIR}"

