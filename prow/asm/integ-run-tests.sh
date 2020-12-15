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

# shellcheck source=prow/asm/asm-lib.sh
source "${WD}/asm-lib.sh"

export BUILD_WITH_CONTAINER=0

# CA = CITADEL, MESHCA or PRIVATECA
CA="MESHCA"
# WIP(Workload Identity Pool) = GKE or HUB
WIP="GKE"
TEST_TARGET="test.integration.multicluster.kube.presubmit"

while (( "$#" )); do
  case $1 in
    --ca)
      case $2 in
        "CITADEL" | "MESHCA" | "PRIVATECA" )
          CA=$2
          shift 2
          ;;
        *)
          echo "Error: Unsupported CA $2" >&2
          exit 1
          ;;
      esac
      ;;
    --test)
      TEST_TARGET=$2
      shift 2
      ;;
    --wip)
      case $2 in
        "GKE" | "HUB" )
          WIP=$2
          shift 2
          ;;
        *)
          echo "Error: Unsupported Workload Identity Pool $2" >&2
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
echo "Running with CA ${CA} and ${WIP} Workload Identity Pool"

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

# Get all contexts of the clusters.
CONTEXTSTR=$(kubectl config view -o jsonpath="{range .contexts[*]}{.name}{','}{end}")
CONTEXTSTR="${CONTEXTSTR::-1}"
IFS="," read -r -a CONTEXTS <<< "$CONTEXTSTR"

# Set up the private CA if it's using the gke deployer.
if [[ "${DEPLOYER}" == "gke" && "${CA}" == "PRIVATECA" ]]; then
  setup_private_ca "${CONTEXTSTR}"
  add_trap "cleanup_private_ca ${CONTEXTSTR}" EXIT SIGKILL SIGTERM SIGQUIT
fi

# If it's using the gke deployer, use one of the projects to hold the images.
if [[ "${DEPLOYER}" == "gke" ]]; then
  # Use the gcr of the first project to store required images.
  IFS="_" read -r -a VALS <<< "${CONTEXTS[0]}"
  GCR_PROJECT_ID=${VALS[1]}
# Otherwise use the central GCP project to hold these images.
else
  GCR_PROJECT_ID="${CENTRAL_GCP_PROJECT}"
fi
export HUB="gcr.io/${GCR_PROJECT_ID}/asm"
export TAG="BUILD_ID_${BUILD_ID}"

if [[ "${DEPLOYER}" == "gke" ]]; then
  echo "Set permissions to allow the Pods on the GKE clusters to pull images..."
  set_gcp_permissions "${GCR_PROJECT_ID}" "${CONTEXTSTR}"
  add_trap "remove_gcp_permissions ${GCR_PROJECT_ID} ${CONTEXTSTR}" EXIT SIGKILL SIGTERM SIGQUIT
elif [[ "${DEPLOYER}" == "tailorbird" ]]; then
  echo "Set permissions to allow the Pods on the multicloud clusters to pull images..."
  # TODO: remove it if there is a general solution for b/174580152
  set_multicloud_permissions "${GCR_PROJECT_ID}" "${CONTEXTSTR}"
fi

echo "Preparing images..."
prepare_images
add_trap "cleanup_images" EXIT SIGKILL SIGTERM SIGQUIT

if [[ "${WIP}" == "HUB" ]]; then
  echo "Register clusters into the Hub..."
  # Use the first project as the GKE Hub host project
  GKEHUB_PROJECT_ID="${GCR_PROJECT_ID}"
  register_clusters_in_hub "${GKEHUB_PROJECT_ID}" "${CONTEXTS[@]}"
  add_trap "cleanup_hub_setup ${GKEHUB_PROJECT_ID} ${CONTEXTSTR}" EXIT SIGKILL SIGTERM SIGQUIT
fi

echo "Building istioctl..."
build_istioctl

echo "Installing ASM control plane..."
gcloud components install kpt
if [[ "${DEPLOYER}" == "gke" ]]; then
  install_asm "${WD}/pkg" "${CA}" "${WIP}" "${CONTEXTS[@]}"
elif [[ "${DEPLOYER}" == "tailorbird" ]]; then
  install_asm_on_multicloud "${WD}/pkg" "CITADEL" "${WIP}" "${CONTEXTS[@]}"
fi

echo "Processing kubeconfig files for running the tests..."
process_kubeconfigs

export KUBECONFIGINPUT="${KUBECONFIG/:/,}"

# exported GCR_PROJECT_ID_1, GCR_PROJECT_ID_2, WIP and CA values are needed for security test.
export GCR_PROJECT_ID_1=${GCR_PROJECT_ID}
IFS="_" read -r -a VALS_2 <<< "${CONTEXTS[1]}"
export GCR_PROJECT_ID_2=${VALS_2[1]}
# When HUB Workload Identity Pool is used in the case of multi projects setup, clusters in different projects
# will use the same WIP and P4SA of the Hub host project.
if [[ "${WIP}" == "HUB" ]]; then
  export GCR_PROJECT_ID_2="${GCR_PROJECT_ID_1}"
fi
export CA
export WIP

# DISABLED_TESTS contains a list of all tests we skip
# pilot/ tests
DISABLED_TESTS="TestWait|TestVersion|TestProxyStatus" # UNSUPPORTED: istioctl doesn't work
DISABLED_TESTS+="|TestAnalysisWritesStatus" # UNSUPPORTED: require custom installation
# telemetry/ tests
DISABLED_TESTS+="|TestDashboard" # UNSUPPORTED: Relies on istiod in cluster. TODO: filter out only pilot-dashboard.json
DISABLED_TESTS+="|TestCustomizeMetrics|TestStatsFilter|TestTcpMetric|TestWasmStatsFilter|TestWASMTcpMetric" # UNKNOWN: b/177606974
# security/ tests

export INTEGRATION_TEST_FLAGS="${INTEGRATION_TEST_FLAGS:-}"

# TODO(nmittler): Remove this once we no longer run the multicluster tests.
export TEST_SELECT="${TEST_SELECT:-}"
if [[ $TEST_TARGET == "test.integration.multicluster.kube.presubmit" ]]; then
  TEST_SELECT="+multicluster"
fi

# Don't deploy Istio. Instead just use the pre-installed ASM
INTEGRATION_TEST_FLAGS+=" --istio.test.kube.deploy=false"

# Don't run VM tests. Echo deployment requires the eastwest gateway
# which isn't deployed for all configurations.
INTEGRATION_TEST_FLAGS+=" --istio.test.skipVM"

# Skip the tests that are known to be not working.
INTEGRATION_TEST_FLAGS+=" --istio.test.skip=\"${DISABLED_TESTS}\""

export INTEGRATION_TEST_KUBECONFIG="${KUBECONFIGINPUT}"

echo "Running e2e test: ${TEST_TARGET}..."
export JUNIT_OUT="${ARTIFACTS}/junit1.xml"
make "${TEST_TARGET}"
