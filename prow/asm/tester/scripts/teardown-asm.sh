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

if [[ "${CONTROL_PLANE}" == "UNMANAGED" ]]; then
  # Clean up the private CA.
  if [[ "${CLUSTER_TYPE}" == "gke" && "${CA}" == "PRIVATECA" ]]; then
    cleanup_private_ca "${CONTEXT_STR}"
  fi

  if [[ "${WIP}" == "HUB" ]]; then
    # Use the first project as the GKE Hub host project
    GKEHUB_PROJECT_ID="${GCR_PROJECT_ID}"
    cleanup_hub_setup "${GKEHUB_PROJECT_ID}" "${CONTEXT_STR}"
  fi

  # Do not clean up the images for multicloud tests, since they are using a
  # shared GCR to store the images, cleaning them up could lead to race
  # conditions for jobs that are running in parallel
  # TODO(chizhg): figure out a way to still clean them up and also avoid the race
  # conditions, potentially using the project rental pool to ensure the
  # isolation.
  if [[ "${CLUSTER_TYPE}" == "gke" ]]; then
    cleanup_images
  fi
else
  cleanup_images_for_managed_control_plane
fi
