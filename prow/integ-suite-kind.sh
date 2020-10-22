#!/bin/bash

# Copyright 2019 Istio Authors
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


# Usage: ./integ-suite-kind.sh TARGET
# Example: ./integ-suite-kind.sh test.integration.pilot.kube.presubmit

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x


# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
setup_and_export_git_sha

TOPOLOGY=SINGLE_CLUSTER

PARAMS=()

if [ "${1:-}" == "test.integration.pilot.kube.presubmit" ]; then
  NODE_IMAGE="kindest/node:v1.16.9"
elif [ "${1:-}" == "test.integration.galley.kube.presubmit" ]; then
  NODE_IMAGE="kindest/node:v1.17.5"
elif [ "${1:-}" == "test.integration.mixer.kube.presubmit" ]; then
  NODE_IMAGE="gcr.io/istio-testing/kind-node:v1.19.0-rc.1"
fi

# KinD will not have a LoadBalancer, so we need to disable it
export TEST_ENV=kind

# See https://kind.sigs.k8s.io/docs/user/quick-start/#loading-an-image-into-your-cluster
export PULL_POLICY=IfNotPresent

# We run a local-registry in a docker container that KinD nodes pull from
# These values are must match what is in config/trustworthy-jwt.yaml
export KIND_REGISTRY_NAME="kind-registry"
export KIND_REGISTRY_PORT="5000"
export KIND_REGISTRY="localhost:${KIND_REGISTRY_PORT}"

export HUB=${HUB:-"istio-testing"}
export TAG="${TAG:-"istio-testing"}"

# If we're not intending to pull from an actual remote registry, use the local kind registry
if [[ -z "${SKIP_BUILD:-}" ]]; then
  HUB="${KIND_REGISTRY}/$(echo "${HUB}" | sed 's/[^\/]*\/\([^\/]*\/\)/\1/')"
  export HUB
fi

# Default IP family of the cluster is IPv4
export IP_FAMILY="${IP_FAMILY:-ipv4}"

# Setup junit report and verbose logging
export T="${T:-"-v"}"
export CI="true"

make init

if [[ -z "${SKIP_SETUP:-}" ]]; then
  if [[ "${TOPOLOGY}" == "SINGLE_CLUSTER" ]]; then
    time setup_kind_cluster "${IP_FAMILY}" "${NODE_IMAGE:-}"
  else
    # TODO: Support IPv6 multicluster
    time setup_kind_clusters "${TOPOLOGY}" "${NODE_IMAGE:-}"

    # Set the kube configs to point to the clusters.
    export INTEGRATION_TEST_KUBECONFIG="${CLUSTER1_KUBECONFIG},${CLUSTER2_KUBECONFIG},${CLUSTER3_KUBECONFIG}"
    export INTEGRATION_TEST_NETWORKS="0:test-network-0,1:test-network-0,2:test-network-1"
    export INTEGRATION_TEST_CONTROLPLANE_TOPOLOGY="0:0,1:0,2:2"
  fi
fi

if [[ -z "${SKIP_BUILD:-}" ]]; then
  time setup_kind_registry
  time build_images "${PARAMS[*]}"
fi

# If a variant is defined, update the tag accordingly
if [[ -n "${VARIANT:-}" ]]; then
  export TAG="${TAG}-${VARIANT}"
fi

make test.integration.kube.presubmit

# Check if the user is running the clusters in manual mode.
if [[ -n "${MANUAL:-}" ]]; then
  echo "Running cluster(s) in manual mode. Press any key to shutdown and exit..."
  read -rsn1
  exit 0
fi
