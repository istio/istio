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

# shellcheck source=prow/asm/tester/scripts/libs/vm-lib.sh
source "${WD}/libs/vm-lib.sh"

# holds multiple kubeconfigs for Multicloud test environments
declare -a MC_CONFIGS
# shellcheck disable=SC2034
IFS=':' read -r -a MC_CONFIGS <<< "${KUBECONFIG}"

echo "======= env vars ========"
printenv

# Get all contexts of the clusters into an array
IFS="," read -r -a CONTEXTS <<< "${CONTEXT_STR}"

# The Makefile passes the path defined in INTEGRATION_TEST_TOPOLOGY_FILE to --istio.test.kube.topology on go test.
export INTEGRATION_TEST_TOPOLOGY_FILE
INTEGRATION_TEST_TOPOLOGY_FILE="${ARTIFACTS}/integration_test_topology.yaml"
if [[ "${CLUSTER_TYPE}" == "gke" ]]; then
  gen_topology_file "${INTEGRATION_TEST_TOPOLOGY_FILE}" "${CONTEXTS[@]}"
else
  multicloud::gen_topology_file "${INTEGRATION_TEST_TOPOLOGY_FILE}"
fi

if [[ "${CONTROL_PLANE}" == "UNMANAGED" ]]; then
  if [ -n "${STATIC_VMS}" ] || "${GCE_VMS}"; then
    echo "Setting up GCP VMs to test against"
    VM_CTX="${CONTEXTS[0]}"
    # ASM_VM_BRANCH is one of the branches in https://github.com/GoogleCloudPlatform/anthos-service-mesh-packages
    ASM_VM_BRANCH="master"
    curl -O https://raw.githubusercontent.com/GoogleCloudPlatform/anthos-service-mesh-packages/"${ASM_VM_BRANCH}"/scripts/asm-installer/asm_vm
    chmod +x asm_vm
    VM_SCRIPT="$PWD/asm_vm"
    export VM_SCRIPT

    export AGENT_BUCKET="gs://gce-service-proxy-canary/service-proxy-agent/releases/service-proxy-agent-staging-latest.tgz"

    [ -n "${STATIC_VMS}" ] && setup_asm_vms "${STATIC_VMS}" "${VM_CTX}" "${VM_DISTRO}" "${IMAGE_PROJECT}"
    [ -n "${STATIC_VMS}" ] && static_vm_topology_entry "${INTEGRATION_TEST_TOPOLOGY_FILE}" "${VM_CTX}"
    "${GCE_VMS}"  && setup_gce_vms "${INTEGRATION_TEST_TOPOLOGY_FILE}" "${VM_CTX}"
  fi
else
  if [[ "${CLUSTER_TOPOLOGY}" == "SINGLECLUSTER" || "${CLUSTER_TOPOLOGY}" == "sc" ]]; then
    echo "Processing kubeconfig files for running the tests..."
    process_kubeconfigs
  fi
fi
