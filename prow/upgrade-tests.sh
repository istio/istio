#!/bin/bash

# Copyright 2018 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

# This is currently triggered by https://github.com/istio/test-infra for release qualification.

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# Set up inputs needed by /istio/istio/tests/upgrade/test_crossgrade.sh, the default settings is to test upgrade from
# 1.3.0 to 1.3.4 using helm chart
# These environment variables are passed by /istio/test-infra/prow/cluster/jobs istio periodic upgrade jobs
export SOURCE_HUB=${SOURCE_HUB:-"docker.io/istio"}
export SOURCE_TAG=${SOURCE_TAG:-"1.3.0"}
export SOURCE_RELEASE_PATH=${SOURCE_RELEASE_PATH:-"https://github.com/istio/istio/releases/download/${SOURCE_TAG}"}
export TARGET_HUB=${TARGET_HUB:-"docker.io/istio"}
export TARGET_TAG=${TARGET_TAG:-"1.3.4"}
export TARGET_RELEASE_PATH=${TAGET_RELEASE_PATH:-"https://github.com/istio/istio/releases/download/${TARGET_TAG}"}
export INSTALL_OPTIONS=${ISTALL_OPTIONS:-"helm"}
export FROM_PATH=${FROM_PATH:-"$(mktemp -d from_dir.XXXXXX)"}
export TO_PATH=${TO_PATH:-"$(mktemp -d to_dir.XXXXXX)"}

# Set to any non-empty value to use kubectl configured cluster instead of mason provisioned cluster.
UPGRADE_TEST_LOCAL="${UPGRADE_TEST_LOCAL:-""}"

echo "Testing upgrade and downgrade between ${SOURCE_HUB}/${SOURCE_TAG} and ${TARGET_HUB}/${TARGET_TAG}"

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"

# Download release artifacts.
download_untar_istio_release "${SOURCE_RELEASE_PATH}" "${SOURCE_TAG}" "${FROM_PATH}"
download_untar_istio_release "${TARGET_RELEASE_PATH}" "${TARGET_TAG}" "${TO_PATH}"

# Check https://github.com/istio/test-infra/blob/master/boskos/resources.yaml
# for existing resources types
if [ "${UPGRADE_TEST_LOCAL}" = "" ]; then
    export RESOURCE_TYPE="${RESOURCE_TYPE:-gke-e2e-test}"
    export OWNER='upgrade-tests'
    export USE_MASON_RESOURCE="${USE_MASON_RESOURCE:-True}"
    export CLEAN_CLUSTERS="${CLEAN_CLUSTERS:-True}"

    setup_e2e_cluster
else
    echo "Running against cluster that kubectl is configured for."
fi

# Install fortio which is needed by the upgrade test.
go get fortio.org/fortio

# Kick off tests
"${ROOT}/tests/upgrade/test_crossgrade.sh" \
  --from_hub="${SOURCE_HUB}" --from_tag="${SOURCE_TAG}" --from_path="${FROM_PATH}/istio-${SOURCE_TAG}" \
  --to_hub="${TARGET_HUB}" --to_tag="${TARGET_TAG}" --to_path="${TO_PATH}/istio-${TARGET_TAG}" \
  --install_options="${INSTALL_OPTIONS}" --cloud="GKE"

