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

readonly MULTIPROJECT_MULTICLUSTER_BOSKOS_RESOURCE="mm-gke-project"

# The network and firewall rule resources are very likely leaked if we
# teardown them with `kubetest2 --down`, so we leverage boskos-janitor here
# since it can make sure the projects can be back to the sanity state.
# TODO(chizhg): find a cleaner and less hacky way to handle this.
function multiproject_multicluster_setup() {
  # Acquire 3 projects for multiproject multicluster tests.
  for ((i=0;i<3;i++));
  do
    boskos_acquire "${MULTIPROJECT_MULTICLUSTER_BOSKOS_RESOURCE}"
  done
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
  echo "$( jq -r .name <<<"${resource}" )"
}

# Release a leased boskos resource.
# Parameters: $1 - resource name. Must be the same as returned by the
#                  boskos_acquire function call, e.g. asm-boskos-1.
function boskos_release() {
  local resource_name="$1"
  boskos_cmd release --name "${resource_name}" --target-state dirty
}
