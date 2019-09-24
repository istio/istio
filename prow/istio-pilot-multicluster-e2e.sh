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

set -eux

set -eux

function setup_clusterreg () {
    MAIN_CONFIG=""
    for context in "${CLUSTERREG_DIR}"/*; do
        if [[ -z "${MAIN_CONFIG}" ]]; then
            MAIN_CONFIG="${context}"
        fi
        export KUBECONFIG="${context}"
        kubectl delete ns istio-system-multi --ignore-not-found
        kubectl delete clusterrolebinding istio-multi-test --ignore-not-found
        kubectl create ns istio-system-multi
        kubectl create sa istio-multi-test -n istio-system-multi
        kubectl create clusterrolebinding istio-multi-test --clusterrole=cluster-admin --serviceaccount=istio-system-multi:istio-multi-test
        CLUSTER_NAME=$(kubectl config view --minify=true -o "jsonpath={.clusters[].name}")
        gen_kubeconf_from_sa istio-multi-test "${context}"
    done
    export KUBECONFIG="${MAIN_CONFIG}"
}

function gen_kubeconf_from_sa () {
    local service_account=$1
    local filename=$2

    SERVER=$(kubectl config view --minify=true -o "jsonpath={.clusters[].cluster.server}")
    SECRET_NAME=$(kubectl get sa "${service_account}" -n istio-system-multi -o jsonpath='{.secrets[].name}')
    CA_DATA=$(kubectl get secret "${SECRET_NAME}" -n istio-system-multi -o "jsonpath={.data['ca\\.crt']}")
    TOKEN=$(kubectl get secret "${SECRET_NAME}" -n istio-system-multi -o "jsonpath={.data['token']}" | base64 --decode)

    cat <<EOF > "${filename}"
      apiVersion: v1
      clusters:
         - cluster:
             certificate-authority-data: ${CA_DATA}
             server: ${SERVER}
           name: ${CLUSTER_NAME}
      contexts:
         - context:
             cluster: ${CLUSTER_NAME}
             user: ${CLUSTER_NAME}
           name: ${CLUSTER_NAME}
      current-context: ${CLUSTER_NAME}
      kind: Config
      preferences: {}
      users:
         - name: ${CLUSTER_NAME}
           user:
             token: ${TOKEN}
EOF
}

SUBNET_A="10.10.0.0/16"
SUBNET_B="10.20.0.0/16"

CONFIG_A=$(cat <<EOF
kind: Cluster
apiVersion: kind.sigs.k8s.io/v1alpha3
networking:
  podSubnet: ${SUBNET_A}
  serviceSubnet: 10.255.10.0/24
EOF
)
CONFIG_B=$(cat <<EOF
kind: Cluster
apiVersion: kind.sigs.k8s.io/v1alpha3
networking:
  podSubnet: ${SUBNET_B}
  serviceSubnet: 10.255.10.0/24
EOF
)

function cleanup_kind_cluster() {
  echo "Test exited with exit code $?."
  kind export logs --name "${1}" "${ARTIFACTS}/kind-${1}/"
  if [[ -z "${SKIP_CLEANUP:-}" ]]; then
    echo "Cleaning up kind cluster"
    kind delete cluster --name="${1}"
  fi
}

# Create two clusters. Explicitly delcare the subnet so we can connect the two later
kind create cluster --name a --config <(echo "${CONFIG_A}")
trap 'cleanup_kind_cluster a' EXIT
kind create cluster --name b --config <(echo "${CONFIG_B}")
trap 'cleanup_kind_cluster b' EXIT

export HUB=istio-testing
export TAG=istio-testing

# Build just the images needed for the tests
for image in pilot proxyv2 app test_policybackend mixer citadel galley sidecar_injector kubectl node-agent-k8s; do
   make docker.${image}
done

# Set up routing rules for inter-cluster direct pod to pod communication
DOCKER_IP_A=$(docker inspect -f "{{ .NetworkSettings.IPAddress }}" a-control-plane)
DOCKER_IP_B=$(docker inspect -f "{{ .NetworkSettings.IPAddress }}" b-control-plane)
POD_CIDR_A=$(KUBECONFIG="$(kind get kubeconfig-path --name a)" kubectl get node -ojsonpath='{.items[0].spec.podCIDR}')
POD_CIDR_B=$(KUBECONFIG="$(kind get kubeconfig-path --name b)" kubectl get node -ojsonpath='{.items[0].spec.podCIDR}')
docker exec a-control-plane ip route add "${POD_CIDR_B}" via "${DOCKER_IP_B}"
docker exec b-control-plane ip route add "${POD_CIDR_A}" via "${DOCKER_IP_A}"

# Load images into both cluster
docker images "${HUB}/*:${TAG}" --format '{{.Repository}}:{{.Tag}}' | xargs -n1 kind --loglevel debug --name a load docker-image
docker images "${HUB}/*:${TAG}" --format '{{.Repository}}:{{.Tag}}' | xargs -n1 kind --loglevel debug --name b load docker-image

# Set up cluster registry
CLUSTERREG_DIR="$(mktemp -d)"
kind get kubeconfig --name a --internal > "${CLUSTERREG_DIR}"/a
kind get kubeconfig --name b --internal > "${CLUSTERREG_DIR}"/b
setup_clusterreg

./prow/e2e-kind-suite.sh \
  --skip-setup --skip-cleanup --skip-build \
  --timeout 90m --cluster_registry_dir="$CLUSTERREG_DIR" --single_test e2e_pilotv2_v1alpha3 "$@"
