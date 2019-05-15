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


#######################################################
# e2e-suite runs Istio E2E tests.                     #
#                                                     #
# Usage: ./e2e_suite.sh --single_test mixer_e2e       #
#                                                     #
# ${E2E_ARGS} can be used to provide additional test  #
# arguments.                                          #
#######################################################

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
export RESOURCE_TYPE="${RESOURCE_TYPE:-gke-e2e-test}"
export OWNER="${OWNER:-e2e-suite}"
export PILOT_CLUSTER="${PILOT_CLUSTER:-}"
export USE_MASON_RESOURCE="${USE_MASON_RESOURCE:-True}"
export CLEAN_CLUSTERS="${CLEAN_CLUSTERS:-True}"

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
setup_e2e_cluster

if [[ "${ENABLE_ISTIO_CNI:-false}" == true ]]; then
   cni_run_daemon
fi

E2E_ARGS+=("--test_logs_path=${ARTIFACTS_DIR}")
# e2e tests on prow use clusters borrowed from boskos, which cleans up the
# clusters. There is no need to cleanup in the test jobs.
E2E_ARGS+=("--skip_cleanup")

export HUB=${HUB:-"gcr.io/istio-testing"}
export TAG="${TAG:-${GIT_SHA}}"

make init

# getopts only handles single character flags
for ((i=1; i<=$#; i++)); do
    case ${!i} in
        # -s/--single_test to specify test target to run.
        # e.g. "-s e2e_mixer" will trigger e2e mixer_test
        -s|--single_test) ((i++)); SINGLE_TEST=${!i}
        continue
        ;;
        --timeout) ((i++)); E2E_TIMEOUT=${!i}
        continue
        ;;
    esac
    E2E_ARGS+=( "${!i}" )
done

time ISTIO_DOCKER_HUB=$HUB \
  E2E_ARGS="${E2E_ARGS[*]}" \
  JUNIT_E2E_XML="${ARTIFACTS_DIR}/junit.xml" \
  make with_junit_report TARGET="${SINGLE_TEST}" ${E2E_TIMEOUT:+ E2E_TIMEOUT="${E2E_TIMEOUT}"}
