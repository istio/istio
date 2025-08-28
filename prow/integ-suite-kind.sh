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
NODE_IMAGE="gcr.io/istio-testing/kind-node:v1.34.0"
KIND_CONFIG=""
CLUSTER_TOPOLOGY_CONFIG_FILE="${ROOT}/prow/config/topology/multicluster.json"
CLUSTER_NAME="${CLUSTER_NAME:-istio-testing}"

export FAST_VM_BUILDS=true
export ISTIO_DOCKER_BUILDER="${ISTIO_DOCKER_BUILDER:-crane}"
# DEVCONTAINER controls a set of features that allow this script to be run from
# within a dev container using ghcr.io/devcontainers/features/docker-outside-of-docker
export DEVCONTAINER="${DEVCONTAINER:-}"
if [[ "${DEVCONTAINER}" ]]; then
  export ISTIO_DOCKER_BUILDER=docker
fi

PARAMS=()

while (( "$#" )); do
  case "$1" in
    # Node images can be found at https://github.com/kubernetes-sigs/kind/releases
    # For example, kindest/node:v1.14.0
    --node-image)
      NODE_IMAGE=$2
      shift 2
    ;;
    # Config for enabling different Kubernetes features in KinD (see prow/config{endpointslice.yaml,trustworthy-jwt.yaml}).
    --kind-config)
    KIND_CONFIG=$2
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
        SINGLE_CLUSTER | MULTICLUSTER_SINGLE_NETWORK | MULTICLUSTER | AMBIENT_MULTICLUSTER )
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
      CLUSTER_TOPOLOGY_CONFIG_FILE="${ROOT}/${2}"
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

if [ -f /proc/cpuinfo ]; then
  echo "Checking CPU..."
  grep 'model' /proc/cpuinfo || true
fi

# Default IP family of the cluster is IPv4
KIND_IP_FAMILY="ipv4"
export IP_FAMILIES="${IP_FAMILIES:-IPv4}"
if [[ "$IP_FAMILIES" == "IPv6" ]]; then
   KIND_IP_FAMILY="ipv6"
elif [[ "$IP_FAMILIES" =~ "IPv6" ]] && [[ "$IP_FAMILIES" =~ "IPv4" ]]; then
   KIND_IP_FAMILY="dual"
fi
export KIND_IP_FAMILY

# LoadBalancer in Kind is supported using metallb
export TEST_ENV=kind-metallb

# See https://kind.sigs.k8s.io/docs/user/quick-start/#loading-an-image-into-your-cluster
export PULL_POLICY=IfNotPresent

# We run a local-registry in a docker container that KinD nodes pull from
# These values are must match what is in config/trustworthy-jwt.yaml
export KIND_REGISTRY_NAME="kind-registry"
export KIND_REGISTRY_PORT="5000"
export KIND_REGISTRY="localhost:${KIND_REGISTRY_PORT}"
export KIND_REGISTRY_DIR="/etc/containerd/certs.d/localhost:${KIND_REGISTRY_PORT}"

export HUB=${HUB:-"istio-testing"}
export TAG="${TAG:-"istio-testing"}"
export VARIANT

# If we're not intending to pull from an actual remote registry, use the local kind registry
if [[ -z "${SKIP_BUILD:-}" ]]; then
  HUB="${KIND_REGISTRY}"
  export HUB
fi

# Setup junit report and verbose logging
export T="${T:-"-v -count=1"}"
export CI="true"

export ARTIFACTS="${ARTIFACTS:-$(mktemp -d)}"
trace "init" make init

if [[ -z "${SKIP_SETUP:-}" ]]; then
  export DEFAULT_CLUSTER_YAML="./prow/config/default.yaml"
  export METRICS_SERVER_CONFIG_DIR='./prow/config/metrics'

  if [[ "${TOPOLOGY}" == "SINGLE_CLUSTER" ]]; then
    trace "setup kind cluster" setup_kind_cluster_retry "${CLUSTER_NAME}" "${NODE_IMAGE}" "${KIND_CONFIG}"
  else
    trace "load cluster topology" load_cluster_topology "${CLUSTER_TOPOLOGY_CONFIG_FILE}"
    trace "setup kind clusters" setup_kind_clusters "${NODE_IMAGE}" "${KIND_IP_FAMILY}"

    TOPOLOGY_JSON=$(cat "${CLUSTER_TOPOLOGY_CONFIG_FILE}")
    for i in $(seq 0 $((${#CLUSTER_NAMES[@]} - 1))); do
      CLUSTER="${CLUSTER_NAMES[i]}"
      KCONFIG="${KUBECONFIGS[i]}"
      TOPOLOGY_JSON=$(set_topology_value "${TOPOLOGY_JSON}" "${CLUSTER}" "meta.kubeconfig" "${KCONFIG}")
    done
    RUNTIME_TOPOLOGY_CONFIG_FILE="${ARTIFACTS}/topology-config.json"
    echo "${TOPOLOGY_JSON}" > "${RUNTIME_TOPOLOGY_CONFIG_FILE}"

    export INTEGRATION_TEST_TOPOLOGY_FILE
    INTEGRATION_TEST_TOPOLOGY_FILE="${RUNTIME_TOPOLOGY_CONFIG_FILE}"

    export INTEGRATION_TEST_KUBECONFIG
    INTEGRATION_TEST_KUBECONFIG=NONE
  fi
fi

if [[ -z "${SKIP_BUILD:-}" ]]; then
  trace "setup kind registry" setup_kind_registry
  trace "build images" build_images "${PARAMS[*]}"

  # upload WASM plugins to kind-registry
  registry_url=$(if [ -z "$DEVCONTAINER" ]; then echo "localhost"; else echo $KIND_REGISTRY_NAME; fi):$KIND_REGISTRY_PORT
  crane copy gcr.io/istio-testing/wasm/attributegen:359dcd3a19f109c50e97517fe6b1e2676e870c4d "$registry_url/istio-testing/wasm/attributegen:0.0.1" --insecure
  crane copy gcr.io/istio-testing/wasm/header-injector:0.0.1 "$registry_url/istio-testing/wasm/header-injector:0.0.1" --insecure
  crane copy gcr.io/istio-testing/wasm/header-injector:0.0.2 "$registry_url/istio-testing/wasm/header-injector:0.0.2" --insecure

  # Make "kind-registry" resolvable in IPv6 cluster
  if [[ "$KIND_IP_FAMILY" == "ipv6" ]]; then
    kind_registry_ip=$(docker inspect -f '{{range $k, $v := .NetworkSettings.Networks}}{{if eq $k "kind"}}{{.GlobalIPv6Address}}{{end}}{{end}}' kind-registry)
    coredns_config=$(kubectl get -oyaml -n=kube-system configmap/coredns)
    echo "Current CoreDNS config:"
    echo "${coredns_config}"
    patched_coredns_config=$(kubectl get -oyaml -n=kube-system configmap/coredns | sed -e '/^ *ready/i\
        hosts {\
            '"$kind_registry_ip"' kind-registry.lan\
            '"$kind_registry_ip"' kind-registry.\
            fallthrough\
        }')
    echo "Patched CoreDNS config:"
    echo "${patched_coredns_config}"
    printf '%s' "${patched_coredns_config}" | kubectl apply -f -
  fi
  # CoreDNS by default caches Kubernetes objects for 30s. This leads to problematic timing issues when we tear down + re-install
  # in our tests. We will negative-cache the object, adding up to 30s on each test suite.
  # See https://github.com/coredns/coredns/pull/2348
  kubectl get -oyaml -n=kube-system configmap/coredns | sed 's/ttl 30/ttl 0/g' | kubectl apply -f -
fi

# Run the test target if provided.
if [[ -n "${PARAMS:-}" ]]; then
  trace "test" make "${PARAMS[*]}"
fi

# Check if the user is running the clusters in manual mode.
if [[ -n "${MANUAL:-}" ]]; then
  echo "Running cluster(s) in manual mode. Press any key to shutdown and exit..."
  read -rsn1
  exit 0
fi
