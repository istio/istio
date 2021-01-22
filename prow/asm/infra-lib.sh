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

# Upgrade the GKE clusters to a given version.
# Parameters: $1 - the Kubernetes release version to which to upgrade the cluster
#             $2 - array of k8s contexts
function upgrade_gke_clusters() {
  local target_cluster_version=$1; shift
  local contexts=("${@}")

  for i in "${!contexts[@]}"; do
    IFS="_" read -r -a vals <<< "${contexts[$i]}"
    local project_id="${vals[1]}"
    local region="${vals[2]}"
    local cluster="${vals[3]}"
    # Run the gcloud upgrade command in the background, for now only upgrade GKE master.
    gcloud container clusters upgrade "${cluster}" --cluster-version="${target_cluster_version}" \
      --project="${project_id}" --region="${region}" --master --quiet &
  done
  # Wait until all the upgrades finishes.
  wait
}
