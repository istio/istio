#!/bin/bash

# Copyright 2018 Istio Authors
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

# Cluster names for multicluster configurations.
export CLUSTER1_NAME=${CLUSTER1_NAME:-"cluster1"}
export CLUSTER2_NAME=${CLUSTER2_NAME:-"cluster2"}
export CLUSTER3_NAME=${CLUSTER3_NAME:-"cluster3"}

export CLUSTER_NAMES=("${CLUSTER1_NAME}" "${CLUSTER2_NAME}" "${CLUSTER3_NAME}")
export CLUSTER_POD_SUBNETS=(10.10.0.0/16 10.20.0.0/16 10.30.0.0/16)
export CLUSTER_SVC_SUBNETS=(10.255.10.0/24 10.255.20.0/24 10.255.30.0/24)

export ARTIFACTS="${ARTIFACTS:-$(mktemp -d)}"

function setup_gcloud_credentials() {
  if [[ $(command -v gcloud) ]]; then
    gcloud auth configure-docker -q
  elif [[ $(command -v docker-credential-gcr) ]]; then
    docker-credential-gcr configure-docker
  else
    echo "No credential helpers found, push to docker may not function properly"
  fi
}

function setup_and_export_git_sha() {
  if [[ -n "${CI:-}" ]]; then

    if [ -z "${PULL_PULL_SHA:-}" ]; then
      if [ -z "${PULL_BASE_SHA:-}" ]; then
        GIT_SHA="$(git rev-parse --verify HEAD)"
        export GIT_SHA
      else
        export GIT_SHA="${PULL_BASE_SHA}"
      fi
    else
      export GIT_SHA="${PULL_PULL_SHA}"
    fi
  else
    # Use the current commit.
    GIT_SHA="$(git rev-parse --verify HEAD)"
    export GIT_SHA
  fi
  GIT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"
  export GIT_BRANCH
  setup_gcloud_credentials
}

# Download and unpack istio release artifacts.
function download_untar_istio_release() {
  local url_path=${1}
  local tag=${2}
  local dir=${3:-.}
  # Download artifacts
  LINUX_DIST_URL="${url_path}/istio-${tag}-linux.tar.gz"

  wget  -q "${LINUX_DIST_URL}" -P "${dir}"
  tar -xzf "${dir}/istio-${tag}-linux.tar.gz" -C "${dir}"
}

function buildx-create() {
  export DOCKER_CLI_EXPERIMENTAL=enabled
  if ! docker buildx ls | grep -q container-builder; then
    docker buildx create --driver-opt network=host --name container-builder
  fi
  docker buildx use container-builder
}

function build_images() {
  SELECT_TEST="${1}"

  buildx-create

  # Build just the images needed for tests
  targets="docker.pilot docker.proxyv2 "

  # use ubuntu:bionic to test vms by default
  targets+="docker.app docker.app_sidecar_ubuntu_bionic docker.test_policybackend "
  if [[ "${SELECT_TEST}" == "test.integration.pilot.kube" ]]; then
    targets+="docker.app_sidecar_ubuntu_xenial docker.app_sidecar_ubuntu_focal docker.app_sidecar_ubuntu_bionic "
    targets+="docker.app_sidecar_debian_9 docker.app_sidecar_debian_10 docker.app_sidecar_centos_8 "
  fi
  targets+="docker.mixer "
  targets+="docker.operator "
  targets+="docker.install-cni "
  DOCKER_BUILD_VARIANTS="${VARIANT:-default}" DOCKER_TARGETS="${targets}" make dockerx.pushx
}

# Creates a local registry for kind nodes to pull images from. Expects that the "kind" network already exists.
function setup_kind_registry() {
  # create a registry container
  docker run \
    -d --restart=always -p "${KIND_REGISTRY_PORT}:5000" --name "${KIND_REGISTRY_NAME}" \
    registry:2

  # Allow kind nodes to reach the registry
  docker network connect "kind" "${KIND_REGISTRY_NAME}"

  # https://docs.tilt.dev/choosing_clusters.html#discovering-the-registry
  for cluster in $(kind get clusters); do
    # TODO get context/config from existing variables
    kind export kubeconfig --name="${cluster}"
    for node in $(kind get nodes --name="${cluster}"); do
      kubectl annotate node "${node}" "kind.x-k8s.io/registry=localhost:${KIND_REGISTRY_PORT}";
    done
  done
}

function cleanup_kind_cluster() {
  NAME="${1}"
  echo "Test exited with exit code $?."
  kind export logs --name "${NAME}" "${ARTIFACTS}/kind" -v9 || true
  if [[ -z "${SKIP_CLEANUP:-}" ]]; then
    echo "Cleaning up kind cluster"
    kind delete cluster --name "${NAME}" -v9 || true
  fi
}

# Cleans up the clusters created by setup_kind_clusters
function cleanup_kind_clusters() {
  for c in "${CLUSTER_NAMES[@]}"; do
     cleanup_kind_cluster "${c}"
  done
}

function setup_kind_cluster() {
  IP_FAMILY="${1:-ipv4}"
  IMAGE="${2:-kindest/node:v1.18.2}"
  NAME="${3:-istio-testing}"
  CONFIG="${4:-}"
  # Delete any previous KinD cluster
  echo "Deleting previous KinD cluster with name=${NAME}"
  if ! (kind delete cluster --name="${NAME}" -v9) > /dev/null; then
    echo "No existing kind cluster with name ${NAME}. Continue..."
  fi

  # explicitly disable shellcheck since we actually want $NAME to expand now
  # shellcheck disable=SC2064
  trap "cleanup_kind_cluster ${NAME}" EXIT

  # If config not explicitly set, then use defaults
  if [[ -z "${CONFIG}" ]]; then
      # Kubernetes 1.15+
      CONFIG=./prow/config/trustworthy-jwt.yaml
      # Configure the cluster IP Family only for default configs
    if [ "${IP_FAMILY}" = "ipv6" ]; then
      grep 'ipFamily: ipv6' "${CONFIG}" || \
      cat <<EOF >> "${CONFIG}"
networking:
  ipFamily: ipv6
EOF
    fi
  fi

  # Create KinD cluster
  if ! (kind create cluster --name="${NAME}" --config "${CONFIG}" -v9 --retain --image "${IMAGE}" --wait=60s); then
    echo "Could not setup KinD environment. Something wrong with KinD setup. Exporting logs."
    exit 1
  fi

  kubectl apply -f ./prow/config/metrics
}

# Sets up 3 kind clusters. Clusters 1 and 2 are configured for direct pod-to-pod traffic across
# clusters, while cluster 3 is left on a separate network.
function setup_kind_clusters() {
  TOPOLOGY="${1}"
  IMAGE="${2}"

  KUBECONFIG_DIR="$(mktemp -d)"

  # The kind tool will error when trying to create clusters in paralell unless we create the network first
  # TODO remove this when kind support creating multiple clusters in parallel - this will break ipv6
  docker network inspect kind > /dev/null 2>&1 || docker network create -d=bridge -o com.docker.network.bridge.enable_ip_masquerade=true kind


  # Trap replaces any previous trap's, so we need to explicitly cleanup both clusters here
  trap cleanup_kind_clusters EXIT

  function deploy_kind() {
    IDX="${1}"
    CLUSTER_NAME="${CLUSTER_NAMES[$IDX]}"
    CLUSTER_POD_SUBNET="${CLUSTER_POD_SUBNETS[$IDX]}"
    CLUSTER_SVC_SUBNET="${CLUSTER_SVC_SUBNETS[$IDX]}"
    CLUSTER_YAML="${ARTIFACTS}/config-${CLUSTER_NAME}.yaml"
    if [ ! -f "${CLUSTER_YAML}" ]; then
      cp ./prow/config/trustworthy-jwt.yaml "${CLUSTER_YAML}"
      cat <<EOF >> "${CLUSTER_YAML}"
networking:
  podSubnet: ${CLUSTER_POD_SUBNET}
  serviceSubnet: ${CLUSTER_SVC_SUBNET}
EOF
    fi

    CLUSTER_KUBECONFIG="${KUBECONFIG_DIR}/${CLUSTER_NAME}"

    # Create the clusters.
    # TODO: add IPv6
    KUBECONFIG="${CLUSTER_KUBECONFIG}" setup_kind_cluster "ipv4" "${IMAGE}" "${CLUSTER_NAME}" "${CLUSTER_YAML}"

    # Kind currently supports getting a kubeconfig for internal or external usage. To simplify our tests,
    # its much simpler if we have a single kubeconfig that can be used internally and externally.
    # To do this, we can replace the server with the IP address of the docker container
    # https://github.com/kubernetes-sigs/kind/issues/1558 tracks this upstream
    CONTAINER_IP=$(docker inspect "${CLUSTER_NAME}-control-plane" --format "{{ .NetworkSettings.Networks.kind.IPAddress }}")
    kind get kubeconfig --name "${CLUSTER_NAME}" --internal | \
      sed "s/${CLUSTER_NAME}-control-plane/${CONTAINER_IP}/g" > "${CLUSTER_KUBECONFIG}"
  }
  declare -a DEPLOY_KIND_JOBS
  for i in "${!CLUSTER_NAMES[@]}"; do
    deploy_kind "${i}" & DEPLOY_KIND_JOBS+=("${!}")
  done
  for pid in "${DEPLOY_KIND_JOBS[@]}"; do
    wait "${pid}" || exit 1
  done

  # Install MetalLB for LoadBalancer support. Must be done synchronously since METALLB_IPS is shared.
  for CLUSTER_NAME in "${CLUSTER_NAMES[@]}"; do
    install_metallb "${KUBECONFIG_DIR}/${CLUSTER_NAME}"
  done

  # Export variables for the kube configs for the clusters.
  export CLUSTER1_KUBECONFIG="${KUBECONFIG_DIR}/${CLUSTER1_NAME}"
  export CLUSTER2_KUBECONFIG="${KUBECONFIG_DIR}/${CLUSTER2_NAME}"
  export CLUSTER3_KUBECONFIG="${KUBECONFIG_DIR}/${CLUSTER3_NAME}"

  if [[ "${TOPOLOGY}" != "SINGLE_CLUSTER" ]]; then
    # Clusters 1 and 2 are on the same network
    connect_kind_clusters "${CLUSTER1_NAME}" "${CLUSTER1_KUBECONFIG}" "${CLUSTER2_NAME}" "${CLUSTER2_KUBECONFIG}" 1
    # Cluster 3 is on a different network but we still need to set up routing for MetalLB addresses
    connect_kind_clusters "${CLUSTER1_NAME}" "${CLUSTER1_KUBECONFIG}" "${CLUSTER3_NAME}" "${CLUSTER3_KUBECONFIG}" 0
    connect_kind_clusters "${CLUSTER2_NAME}" "${CLUSTER2_KUBECONFIG}" "${CLUSTER3_NAME}" "${CLUSTER3_KUBECONFIG}" 0
  fi
}

function connect_kind_clusters() {
  C1="${1}"
  C1_KUBECONFIG="${2}"
  C2="${3}"
  C2_KUBECONFIG="${4}"
  POD_TO_POD_AND_SERVICE_CONNECTIVITY="${5}"

  C1_NODE="${C1}-control-plane"
  C2_NODE="${C2}-control-plane"
  C1_DOCKER_IP=$(docker inspect -f "{{ .NetworkSettings.Networks.kind.IPAddress }}" "${C1_NODE}")
  C2_DOCKER_IP=$(docker inspect -f "{{ .NetworkSettings.Networks.kind.IPAddress }}" "${C2_NODE}")
  if [ "${POD_TO_POD_AND_SERVICE_CONNECTIVITY}" -eq 1 ]; then
    # Set up routing rules for inter-cluster direct pod to pod & service communication
    C1_POD_CIDR=$(KUBECONFIG="${C1_KUBECONFIG}" kubectl get node -ojsonpath='{.items[0].spec.podCIDR}')
    C2_POD_CIDR=$(KUBECONFIG="${C2_KUBECONFIG}" kubectl get node -ojsonpath='{.items[0].spec.podCIDR}')
    C1_SVC_CIDR=$(KUBECONFIG="${C1_KUBECONFIG}" kubectl cluster-info dump | sed -n 's/^.*--service-cluster-ip-range=\([^"]*\).*$/\1/p' | head -n 1)
    C2_SVC_CIDR=$(KUBECONFIG="${C2_KUBECONFIG}" kubectl cluster-info dump | sed -n 's/^.*--service-cluster-ip-range=\([^"]*\).*$/\1/p' | head -n 1)
    docker exec "${C1_NODE}" ip route add "${C2_POD_CIDR}" via "${C2_DOCKER_IP}"
    docker exec "${C1_NODE}" ip route add "${C2_SVC_CIDR}" via "${C2_DOCKER_IP}"
    docker exec "${C2_NODE}" ip route add "${C1_POD_CIDR}" via "${C1_DOCKER_IP}"
    docker exec "${C2_NODE}" ip route add "${C1_SVC_CIDR}" via "${C1_DOCKER_IP}"
  fi

  # Set up routing rules for inter-cluster pod to MetalLB LoadBalancer communication
  connect_metallb "$C1_NODE" "$C2_KUBECONFIG" "$C2_DOCKER_IP"
  connect_metallb "$C2_NODE" "$C1_KUBECONFIG" "$C1_DOCKER_IP"
}

function install_metallb() {
  KUBECONFIG="${1}"
  kubectl apply --kubeconfig="$KUBECONFIG" -f https://raw.githubusercontent.com/metallb/metallb/v0.9.3/manifests/namespace.yaml
  kubectl apply --kubeconfig="$KUBECONFIG" -f https://raw.githubusercontent.com/metallb/metallb/v0.9.3/manifests/metallb.yaml
  kubectl create --kubeconfig="$KUBECONFIG" secret generic -n metallb-system memberlist --from-literal=secretkey="$(openssl rand -base64 128)"

  if [ -z "${METALLB_IPS[*]}" ]; then
    # Take IPs from the end of the docker kind network subnet to use for MetalLB IPs
    DOCKER_KIND_SUBNET="$(docker inspect kind | jq .[0].IPAM.Config[0].Subnet -r)"
    METALLB_IPS=()
    while read -r ip; do
      METALLB_IPS+=("$ip")
    done < <(cidr_to_ips "$DOCKER_KIND_SUBNET" | tail -n 100)
  fi

  # Give this cluster of those IPs
  RANGE="${METALLB_IPS[0]}-${METALLB_IPS[9]}"
  METALLB_IPS=("${METALLB_IPS[@]:10}")

  echo 'apiVersion: v1
kind: ConfigMap
metadata:
  namespace: metallb-system
  name: config
data:
  config: |
    address-pools:
    - name: default
      protocol: layer2
      addresses:
      - '"$RANGE" | kubectl apply --kubeconfig="$KUBECONFIG" -f -
}

function connect_metallb() {
  REMOTE_NODE=$1
  METALLB_KUBECONFIG=$2
  METALLB_DOCKER_IP=$3

  IP_REGEX='(([0-9]{1,3}\.?){4})'
  LB_CONFIG="$(kubectl --kubeconfig="${METALLB_KUBECONFIG}" -n metallb-system get cm config -o jsonpath="{.data.config}")"
  if [[ "$LB_CONFIG" =~ $IP_REGEX-$IP_REGEX ]]; then
    while read -r lb_cidr; do
      docker exec "${REMOTE_NODE}" ip route add "${lb_cidr}" via "${METALLB_DOCKER_IP}"
    done < <(ips_to_cidrs "${BASH_REMATCH[1]}" "${BASH_REMATCH[3]}")
  fi
}

function cidr_to_ips() {
    CIDR="$1"
    python3 - <<EOF
from ipaddress import IPv4Network; [print(str(ip)) for ip in IPv4Network('$CIDR').hosts()]
EOF
}

function ips_to_cidrs() {
  IP_RANGE_START="$1"
  IP_RANGE_END="$2"
  python3 - <<EOF
from ipaddress import summarize_address_range, IPv4Address
[ print(n.compressed) for n in summarize_address_range(IPv4Address(u'$IP_RANGE_START'), IPv4Address(u'$IP_RANGE_END')) ]
EOF
}

# setup_cluster_reg is used to set up a cluster registry for multicluster testing
function setup_cluster_reg () {
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
