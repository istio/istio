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

# shellcheck source=common/scripts/kind_provisioner.sh
source "${ROOT}/common/scripts/kind_provisioner.sh"

TOPOLOGY=SINGLE_CLUSTER
NODE_IMAGE="kindest/node:v1.18.2"
CLUSTER_TOPOLOGY_CONFIG_FILE="${ROOT}/prow/config/topology/multicluster.json"

PARAMS=()

while (( "$#" )); do
  case "$1" in
    # Node images can be found at https://github.com/kubernetes-sigs/kind/releases
    # For example, kindest/node:v1.14.0
    --node-image)
      NODE_IMAGE=$2
      shift 2
    ;;
    --skip-setup)
      SKIP_SETUP=true
      shift
    ;;
    --skip-cleanup)
      SKIP_CLEANUP=true
      shift
    ;;
    --skip-build)
      SKIP_BUILD=true
      shift
    ;;
    --manual)
      MANUAL=true
      shift
    ;;
    --topology)
      case $2 in
        # TODO(landow) get rid of MULTICLUSTER_SINGLE_NETWORK after updating Prow job
        SINGLE_CLUSTER | MULTICLUSTER_SINGLE_NETWORK | MULTICLUSTER )
          TOPOLOGY=$2
          echo "Running with topology ${TOPOLOGY}"
          ;;
        *)
          echo "Error: Unsupported topology ${TOPOLOGY}" >&2
          exit 1
          ;;
      esac
      shift 2
    ;;
    --topology-config)
      CLUSTER_TOPOLOGY_CONFIG_FILE=$2
      shift 2
    ;;
    -*)
      echo "Error: Unsupported flag $1" >&2
      exit 1
      ;;
    *) # preserve positional arguments
      PARAMS+=("$1")
      shift
      ;;
  esac
done

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
  HUB="${KIND_REGISTRY}"
  export HUB
fi

# Default IP family of the cluster is IPv4
export IP_FAMILY="${IP_FAMILY:-ipv4}"

# Setup junit report and verbose logging
export T="${T:-"-v -count=1"}"
export CI="true"

make init

if [[ -z "${SKIP_SETUP:-}" ]]; then
  export ARTIFACTS="${ARTIFACTS:-$(mktemp -d)}"
  export DEFAULT_CLUSTER_YAML="./prow/config/trustworthy-jwt.yaml"
  export METRICS_SERVER_CONFIG_DIR='./prow/config/metrics'

  if [[ "${TOPOLOGY}" == "SINGLE_CLUSTER" ]]; then
    time setup_kind_cluster 
  else
    time load_cluster_topology "${CLUSTER_TOPOLOGY_CONFIG_FILE}"
    time setup_kind_clusters "${NODE_IMAGE}" "${IP_FAMILY}"

    export TEST_ENV=kind-metallb
    export INTEGRATION_TEST_KUBECONFIG
    INTEGRATION_TEST_KUBECONFIG=$(IFS=','; echo "${KUBECONFIGS[*]}")

    ITER_END=$((NUM_CLUSTERS-1))
    declare -a CONTROLPLANE_TOPOLOGIES
    declare -a CONFIG_TOPOLOGIES
    declare -a NETWORK_TOPOLOGIES

    for i in $(seq 0 $ITER_END); do
      CLUSTER_ITEM=$(jq -r ".[$i]" "${CLUSTER_TOPOLOGY_CONFIG_FILE}")
      CONTROLPLANE_INDEX=$(echo "$CLUSTER_ITEM" | jq -r '.control_plane_index')
      CONFIG_INDEX=$(echo "$CLUSTER_ITEM" | jq -r '.config_index')
      
      CONTROLPLANE_TOPOLOGIES+=("$i:$CONTROLPLANE_INDEX")
      CONFIG_TOPOLOGIES+=("$i:$CONFIG_INDEX")
      NETWORK_TOPOLOGIES+=("$i:test-network-${CLUSTER_NETWORK_ID[$i]}")
    done

    export INTEGRATION_TEST_NETWORKS
    export INTEGRATION_TEST_CONTROLPLANE_TOPOLOGY
    export INTEGRATION_TEST_CONFIG_TOPOLOGY

    INTEGRATION_TEST_NETWORKS=$(IFS=','; echo "${NETWORK_TOPOLOGIES[*]}")
    INTEGRATION_TEST_CONTROLPLANE_TOPOLOGY=$(IFS=','; echo "${CONTROLPLANE_TOPOLOGIES[*]}")
    INTEGRATION_TEST_CONFIG_TOPOLOGY=$(IFS=','; echo "${CONFIG_TOPOLOGIES[*]}")
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

# Run the test target if provided.
if [[ -n "${PARAMS:-}" ]]; then
  make "${PARAMS[*]}"
fi

# Check if the user is running the clusters in manual mode.
if [[ -n "${MANUAL:-}" ]]; then
  echo "Running cluster(s) in manual mode. Press any key to shutdown and exit..."
  read -rsn1
  exit 0
fi
