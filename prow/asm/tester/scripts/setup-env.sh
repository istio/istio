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

# shellcheck disable=SC2034
# holds multiple kubeconfigs for Multicloud test environments
declare -a MC_CONFIGS
# shellcheck disable=SC2034
IFS=':' read -r -a MC_CONFIGS <<< "${KUBECONFIG}"

# Get all contexts of the clusters into an array
# shellcheck disable=SC2034
IFS="," read -r -a CONTEXTS <<< "${CONTEXT_STR}"

if [[ "${CONTROL_PLANE}" == "UNMANAGED" ]]; then
  if [[ "${CLUSTER_TYPE}" == "gke" ]]; then
    echo "Set permissions to allow the Pods on the GKE clusters to pull images..."
    # shellcheck disable=SC2153
    set_gcp_permissions "${GCR_PROJECT_ID}" "${CONTEXT_STR}"
  else
    echo "Set permissions to allow the Pods on the multicloud clusters to pull images..."
    set_multicloud_permissions "${GCR_PROJECT_ID}" "${CONTEXT_STR}"
  fi
fi
