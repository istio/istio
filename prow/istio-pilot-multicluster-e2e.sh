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

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
setup_and_export_git_sha


cat <<EOF > "${ARTIFACTS}/config-local.yaml"
kind: Cluster
apiVersion: kind.sigs.k8s.io/v1alpha3
networking:
  podSubnet: 10.10.0.0/16
  serviceSubnet: 10.255.10.0/24
EOF
cat <<EOF > "${ARTIFACTS}/config-remote.yaml"
kind: Cluster
apiVersion: kind.sigs.k8s.io/v1alpha3
networking:
  podSubnet: 10.20.0.0/16
  serviceSubnet: 10.255.10.0/24
EOF

CLUSTERREG_DIR="$(mktemp -d)"

# Create two clusters. Explicitly delcare the subnet so we can connect the two later
KUBECONFIG="${CLUSTERREG_DIR}/local" setup_kind_cluster "" local "${ARTIFACTS}/config-local.yaml"
KUBECONFIG="${CLUSTERREG_DIR}/remote" setup_kind_cluster "" remote "${ARTIFACTS}/config-remote.yaml"

# Replace with --internal which allows cross-cluster api server access
kind get kubeconfig --name local --internal > "${CLUSTERREG_DIR}"/local
kind get kubeconfig --name remote --internal > "${CLUSTERREG_DIR}"/remote

# Trap replaces any previous trap's, so we need to explicitly cleanup both clusters here
# shellcheck disable=SC2064
trap "cleanup_kind_cluster local; cleanup_kind_cluster remote" EXIT

export HUB=istio-testing
export TAG=istio-testing

time build_images

# Set up routing rules for inter-cluster direct pod to pod communication
DOCKER_IP_LOCAL=$(docker inspect -f "{{ .NetworkSettings.IPAddress }}" local-control-plane)
DOCKER_IP_REMOTE=$(docker inspect -f "{{ .NetworkSettings.IPAddress }}" remote-control-plane)
POD_CIDR_LOCAL=$(KUBECONFIG="${CLUSTERREG_DIR}/local" kubectl get node -ojsonpath='{.items[0].spec.podCIDR}')
POD_CIDR_REMOTE=$(KUBECONFIG="${CLUSTERREG_DIR}/remote" kubectl get node -ojsonpath='{.items[0].spec.podCIDR}')
docker exec local-control-plane ip route add "${POD_CIDR_REMOTE}" via "${DOCKER_IP_REMOTE}"
docker exec remote-control-plane ip route add "${POD_CIDR_LOCAL}" via "${DOCKER_IP_LOCAL}"

# Load images into both cluster
time kind_load_images local
time kind_load_images remote

# Set up cluster registry
setup_cluster_reg

./prow/e2e-kind-suite.sh \
  --skip-setup --skip-cleanup --skip-build \
  --cluster_registry_dir="$CLUSTERREG_DIR" --single_test e2e_pilotv2_v1alpha3 "$@"
