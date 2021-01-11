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

export VPC_SC_BOSKOS_RESOURCE="vpc-sc-gke-project"
export COMMON_BOSKOS_RESOURCE="gke-project"

# Setup the project for VPC-SC testing, as per the instructions in
# https://docs.google.com/document/d/11yYDxxI-fbbqlpvUYRtJiBmGdY_nIKPJLbssM3YQtKI/edit#heading=h.e2laig460f1d
function vpc_sc_project_setup() {
  local project_id
  project_id=$(boskos_acquire "${VPC_SC_BOSKOS_RESOURCE}")

  # Create the route as per the user guide above.
  # Currently only the route needs to be recreated since only it will be cleaned
  # up by Boskos janitor.
  # TODO(chizhg): create everything else from scratch here after we are able to
  # use Boskos janitor to clean them up as well, as per the long-term plan in go/asm-vpc-sc-testing-plan
  gcloud compute routes create "restricted-vip" \
    --network=default \
    --destination-range=199.36.153.4/30 \
    --next-hop-gateway=default-internet-gateway \
    --project="${project_id}" > /dev/null 2>&1

  echo "${project_id}"
}

#####################################################################
# Functions used for boskos (the project rental service) operations #
#####################################################################

# Depends on following env vars
# - JOB_NAME: available in all Prow jobs

# Common boskos arguments are presented once.
function boskos_cmd() {
  boskosctl --server-url "http://boskos.test-pods.svc.cluster.local." --owner-name "${JOB_NAME}" "${@}"
}

# Returns a boskos resource name of the given type. Times out in 10 minutes if it cannot get a clean project.
# 1. Lease the resource from boskos.
# 2. Send a heartbeat in the background to keep the lease active.
# Parameters: $1 - resource type. Must be one of the types configured in https://gke-internal.googlesource.com/istio/test-infra-internal/+/refs/heads/master/boskos/config/resources.yaml.
function boskos_acquire() {
  local resource_type="$1"
  local resource
  resource="$( boskos_cmd acquire --type "${resource_type}" --state free --target-state busy --timeout 10m )"
  if [[ -z ${resource} ]]; then
    return 1
  fi

  # Send a heartbeat in the background to keep the lease while using the resource.
  boskos_cmd heartbeat --resource "${resource}" > /dev/null 2> /dev/null &
  jq -r .name <<<"${resource}"
}

# Release a leased boskos resource.
# Parameters: $1 - resource name. Must be the same as returned by the
#                  boskos_acquire function call, e.g. asm-boskos-1.
function boskos_release() {
  local resource_name="$1"
  boskos_cmd release --name "${resource_name}" --target-state dirty
}

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
