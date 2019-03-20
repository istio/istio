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

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")
# Set to any non-empty value to use kubectl configured cluster instead of mason provisioned cluster.
UPGRADE_TEST_LOCAL="${UPGRADE_TEST_LOCAL:-}"

# This is a script to download release artifacts from monthly or daily release
# location and kick off upgrade/downgrade tests.
#
# This is currently triggered by https://github.com/istio-releases/daily-release
# for release qualification.
#
# Expects HUB, SOURCE_VERSION, TARGET_VERSION, SOURCE_RELEASE_PATH, and TARGET_RELEASE_PATH as inputs.

echo "Testing upgrade and downgrade between ${HUB}/${SOURCE_VERSION} and ${HUB}/${TARGET_VERSION}"

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"

# Download release artifacts.
download_untar_istio_release "${SOURCE_RELEASE_PATH}" "${SOURCE_VERSION}"
download_untar_istio_release "${TARGET_RELEASE_PATH}" "${TARGET_VERSION}"


# Check https://github.com/istio/test-infra/blob/master/boskos/configs.yaml
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
"${ROOT}/tests/upgrade/test_crossgrade.sh" --from_hub="${HUB}" --from_tag="${SOURCE_VERSION}" --from_path="istio-${SOURCE_VERSION}" --to_hub="${HUB}" --to_tag="${TARGET_VERSION}" --to_path="istio-${TARGET_VERSION}" --cloud="GKE"

