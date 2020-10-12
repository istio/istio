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

# This script is mainly responsible for setting up the SUT and running the tests.
# The env vars used here are set by the integ-suite-kubetest2.sh script, which
# is the entrypoint for the test jobs run by Prow.

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

# shellcheck source=prow/asm/lib.sh
source "${WD}/lib.sh"

export BUILD_WITH_CONTAINER=0

# CA = CITADEL or MESHCA
CA="MESHCA"
while (( "$#" )); do
  case $1 in
    --ca)
      case $2 in
        "CITADEL" | "MESHCA" )
          CA=$2
          shift 2
          ;;
        *)
          echo "Error: Unsupported CA $2" >&2
          exit 1
          ;;
      esac
      ;;
    *)
      echo "Error: Unsupported input $1" >&2
      exit 1
      ;;
    esac
done
echo "Running with CA ${CA}"

echo "Using ${KUBECONFIG} to connect to the cluster(s)"
if [[ -z "${KUBECONFIG}" ]]; then
  echo "Error: ${KUBECONFIG} cannot be empty."
  exit 1
fi

echo "The kubetest2 deployer is ${DEPLOYER}"
if [[ -z ${DEPLOYER} ]]; then
  echo "Error: ${DEPLOYER} cannot be empty."
  exit 1
fi

echo "The cluster topology is ${CLUSTER_TOPOLOGY}"
if [[ -z "${CLUSTER_TOPOLOGY}" ]]; then
  echo "Error: ${CLUSTER_TOPOLOGY} cannot be empty."
  exit 1
fi

# We observed the kubeconfig files don't have current-context
# set correctly. Add the logging for help debugging this issue.
# Remove this part once b/168906799 is closed.
echo "Printing kubeconfig files for debugging..."
print_kubeconfigs

# Get all contexts of the clusters.
CONTEXTSTR=$(kubectl config view -o jsonpath="{range .contexts[*]}{.name}{','}{end}")
CONTEXTSTR="${CONTEXTSTR::-1}"
IFS="," read -r -a CONTEXTS <<< "$CONTEXTSTR"

# Use the gcr of the first project to store required images.
IFS="_" read -r -a VALS <<< "${CONTEXTS[0]}"
GCR_PROJECT_ID=${VALS[1]}
export HUB="gcr.io/${GCR_PROJECT_ID}/asm"
export TAG="BUILD_ID_${BUILD_ID}"

echo "Preparing images..."
prepare_images

echo "Set permissions to allow other projects pull images..."
set_permissions "${GCR_PROJECT_ID}" "${CONTEXTS[@]}"

echo "Building istioctl..."
build_istioctl

echo "Installing ASM control plane..."
gcloud components install kpt
install_asm "${WD}/pkg" "${CA}" "${CONTEXTS[@]}"

# We observed the kubeconfig files don't have current-context
# set correctly. Add the logging for help debugging this issue.
# Remove this part once b/168906799 is closed.
echo "Printing kubeconfig files for debugging..."
print_kubeconfigs

echo "Processing kubeconfig files for running the tests..."
process_kubeconfigs

export KUBECONFIGINPUT="${KUBECONFIG/:/,}"

echo "Running e2e test: test.integration.multicluster.kube.presubmit..."
make test.integration.multicluster.kube.presubmit \
  INTEGRATION_TEST_FLAGS="--istio.test.kube.deploy=false" \
  TEST_SELECT="+multicluster" \
  INTEGRATION_TEST_KUBECONFIG="${KUBECONFIGINPUT}"

echo "Cleaning up..."
cleanup_images

# Remove read permissions from the first project.
remove_permissions "${GCR_PROJECT_ID}" "${CONTEXTS[@]}"