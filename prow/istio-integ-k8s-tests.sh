#!/bin/bash

# Copyright 2017 Istio Authors
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

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

# Check https://github.com/istio/test-infra/blob/master/boskos/configs.yaml
# for existing resources types
RESOURCE_TYPE="${RESOURCE_TYPE:-gke-e2e-test}"
OWNER="${OWNER:-integ}"
USE_MASON_RESOURCE="${USE_MASON_RESOURCE:-True}"
CLEAN_CLUSTERS="${CLEAN_CLUSTERS:-True}"


# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
# shellcheck source=prow/mason_lib.sh
source "${ROOT}/prow/mason_lib.sh"
# shellcheck source=prow/cluster_lib.sh
source "${ROOT}/prow/cluster_lib.sh"

function cleanup() {
  if [[ "${CLEAN_CLUSTERS}" == "True" ]]; then
    unsetup_clusters
  fi
  if [[ "${USE_MASON_RESOURCE}" == "True" ]]; then
    mason_cleanup
    cat "${FILE_LOG}"
  fi
}

trap cleanup EXIT

INFO_PATH="$(mktemp /tmp/XXXXX.boskos.info)"
FILE_LOG="$(mktemp /tmp/XXXXX.boskos.log)"

setup_and_export_git_sha

get_resource "${RESOURCE_TYPE}" "${OWNER}" "${INFO_PATH}" "${FILE_LOG}"


if [ "${CI:-}" == 'bootstrap' ]; then
  # bootsrap upload all artifacts in _artifacts to the log bucket.
  ARTIFACTS_DIR=${ARTIFACTS_DIR:-"${GOPATH}/src/istio.io/istio/_artifacts"}
fi

export HUB=${HUB:-"gcr.io/istio-testing"}
export TAG="${TAG:-${GIT_SHA}}"

make init

setup_cluster

JUNIT_UNIT_TEST_XML="${ARTIFACTS_DIR}/junit_unit-tests.xml" \
T="-v" \
make test.integration.kube

