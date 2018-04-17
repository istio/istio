#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

#######################################
# Presubmit script triggered by Prow. #
#######################################

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

# Check https://github.com/istio/test-infra/blob/master/boskos/configs.yaml
# for existing resources types
RESOURCE_TYPE="${RESOURCE_TYPE:-gke-e2e-test}"
OWNER='istio-pilot-e2e'
INFO_PATH="$(mktemp /tmp/XXXXX.boskos.info)"
FILE_LOG="$(mktemp /tmp/XXXXX.boskos.log)"

source "${ROOT}/prow/lib.sh"
source "${ROOT}/prow/mason_lib.sh"
source "${ROOT}/prow/cluster_lib.sh"

function cleanup() {
  mason_cleanup
  cat "${FILE_LOG}"
}

setup_and_export_git_sha

trap cleanup EXIT
get_resource "${RESOURCE_TYPE}" "${OWNER}" "${INFO_PATH}" "${FILE_LOG}"
setup_cluster

HUB="gcr.io/istio-testing"

cd ${GOPATH}/src/istio.io/istio

# Run tests with auth disabled
make depend e2e_pilot HUB="${HUB}" TAG="${GIT_SHA}" TESTOPTS=" -mixer=true -use-sidecar-injector=true -use-admission-webhook=false -auth_enable=false -v1alpha3=true -v1alpha1=false"

# Run tests with auth enabled
make depend e2e_pilot HUB="${HUB}" TAG="${GIT_SHA}" TESTOPTS="-mixer=true -use-sidecar-injector=true -use-admission-webhook=false -auth_enable=true -v1alpha3=true -v1alpha1=false"
