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

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

# shellcheck source=prow/asm/tester/scripts/libs/asm-lib.sh
source "${WD}/libs/asm-lib.sh"
# shellcheck source=prow/asm/tester/scripts/libs/infra-lib.sh
source "${WD}/libs/infra-lib.sh"
# shellcheck source=prow/asm/tester/scripts/libs/revision-lib.sh
source "${WD}/libs/revision-lib.sh"

# shellcheck disable=SC2034
# holds multiple kubeconfigs for Multicloud test environments
declare -a MC_CONFIGS
# shellcheck disable=SC2034
IFS=':' read -r -a MC_CONFIGS <<< "${KUBECONFIG}"

# hold the ENVIRON_PROJECT_ID used for ASM Onprem cluster installation with Hub
declare ENVIRON_PROJECT_ID
# hold the http proxy used for connected to baremetal SC
declare HTTP_PROXY
declare HTTPS_PROXY

# TODO: these should really be part of setup-env
# construct http proxy value for baremetal
[[ "${CLUSTER_TYPE}" == "bare-metal" ]] && init_baremetal_http_proxy
# construct http proxy value for aws
[[ "${CLUSTER_TYPE}" == "aws" ]] && aws::init

# Get all contexts of the clusters into an array
IFS="," read -r -a CONTEXTS <<< "${CONTEXT_STR}"

# TODO(ruigu): extract the common part of MANAGED and UNMANAGED when MANAGED test is added.
if [[ "${CONTROL_PLANE}" == "UNMANAGED" ]]; then
  echo "Setting up ASM ${CONTROL_PLANE} control plane for test"

  echo "Preparing images..."
  prepare_images

  # Set up the private CA if requested.
  if [[ "${CA}" == "PRIVATECA" ]]; then
    setup_private_ca "${CONTEXT_STR}"
  fi

  if [[ "${WIP}" == "HUB" ]]; then
    echo "Register clusters into the Hub..."
    # Use the first project as the GKE Hub host project
    # shellcheck disable=SC2153
    GKEHUB_PROJECT_ID="${GCR_PROJECT_ID}"
    register_clusters_in_hub "${GKEHUB_PROJECT_ID}" "${CONTEXTS[@]}"
  fi

  echo "Building istioctl..."
  build_istioctl

  echo "Installing ASM control plane..."
  if [[ "${CLUSTER_TYPE}" == "gke" ]]; then
    if [ -z "${REVISION_CONFIG_FILE}" ]; then
      install_asm "${CONFIG_DIR}/kpt-pkg" "${CA}" "${WIP}" "" "" "" "${CONTEXTS[@]}"
    else
      install_asm_revisions "${REVISION_CONFIG_FILE}" "${CONFIG_DIR}/kpt-pkg" "${WIP}" "${CONTEXTS[@]}"
    fi
  else
    create_asm_revision_label
    if [[ "${CLUSTER_TYPE}" == "bare-metal" ]]; then
      export HTTP_PROXY
      export HTTPS_PROXY
      install_asm_on_baremetal
      export -n HTTP_PROXY
      export -n HTTPS_PROXY
    else
      install_asm_on_multicloud "${CA}" "${WIP}"
    fi
  fi

  echo "Processing kubeconfig files for running the tests..."
  process_kubeconfigs

  # when CLUSTER_TYPE is gke-on-prem, GCR_PROJECT_ID is set to SHARED_GCP_PROJECT istio-prow-build
  # istio-prow-build is not the environ project
  # ENVIRON_PROJECT_ID is the project ID of the environ project where the Onprem cluster is registered
  if [[ "${CLUSTER_TYPE}" == "gke-on-prem" && "${WIP}" == "HUB" ]]; then
    export GCR_PROJECT_ID_1="${ENVIRON_PROJECT_ID}"
  fi

else
  echo "Setting up ASM ${CONTROL_PLANE} control plane for test"

  echo "Preparing images for managed control plane..."
  prepare_images_for_managed_control_plane

  echo "Building istioctl..."
  build_istioctl

  install_asm_managed_control_plane "${CONTEXTS[@]}"
  for i in "${!CONTEXTS[@]}"; do
    kubectl wait --for=condition=available --timeout=2m -n istio-system deployment/istio-ingressgateway --context="${CONTEXTS[$i]}"
  done
fi
