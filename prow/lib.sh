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

# Cluster names for multicluster
export CLUSTER1_NAME=${CLUSTER1_NAME:-"cluster1"}
export CLUSTER2_NAME=${CLUSTER2_NAME:-"cluster2"}

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
    export ARTIFACTS="${ARTIFACTS:-$(mktemp -d)}"
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

function build_images_legacy() {
  # Build just the images needed for the legacy e2e tests that use the install/ directory
  targets="docker.pilot docker.proxyv2 "
  targets+="docker.app docker.test_policybackend "
  targets+="docker.mixer docker.galley"
  DOCKER_BUILD_VARIANTS="${VARIANT:-default}" DOCKER_TARGETS="${targets}" make dockerx
}

function build_images() {
  # Build just the images needed for tests
  targets="docker.pilot docker.proxyv2 "
  targets+="docker.app docker.test_policybackend "
  targets+="docker.mixer docker.galley "
  DOCKER_BUILD_VARIANTS="${VARIANT:-default}" DOCKER_TARGETS="${targets}" make dockerx
}

function kind_load_images() {
  NAME="${1:-istio-testing}"

  # If HUB starts with "docker.io/" removes that part so that filtering and loading below works
  local hub=${HUB#"docker.io/"}

  for i in {1..3}; do
    # Archived local images and load it into KinD's docker daemon
    # Kubernetes in KinD can only access local images from its docker daemon.
    docker images "${hub}/*:${TAG}" --format '{{.Repository}}:{{.Tag}}' | xargs -n1 kind -v9 --name "${NAME}" load docker-image && break
    echo "Attempt ${i} to load images failed, retrying in 1s..."
    sleep 1
	done

  # If a variant is specified, load those images as well.
  # We should still load non-variant images as well for things like `app` which do not use variants
  if [[ "${VARIANT:-}" != "" ]]; then
    for i in {1..3}; do
      docker images "${hub}/*:${TAG}-${VARIANT}" --format '{{.Repository}}:{{.Tag}}' | xargs -n1 kind -v9 --name "${NAME}" load docker-image && break
      echo "Attempt ${i} to load images failed, retrying in 1s..."
      sleep 1
    done
  fi
}

# Loads images into both $CLUSTER1_NAME and $CLUSTER2_NAME
function kind_load_images_multicluster() {
  time kind_load_images "${CLUSTER1_NAME}"
  time kind_load_images "${CLUSTER2_NAME}"
}

function clone_cni() {
  # Clone the CNI repo so the CNI artifacts can be built.
  if [[ "$PWD" == "${GOPATH}/src/istio.io/istio" ]]; then
      TMP_DIR=$PWD
      cd ../ || return
      git clone -b "${GIT_BRANCH}" "https://github.com/istio/cni.git"
      cd "${TMP_DIR}" || return
  fi
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

# Cleans up the clusters created by setup_kind_multicluster_single_network
function cleanup_kind_multicluster() {
  cleanup_kind_cluster "${CLUSTER1_NAME}"
  cleanup_kind_cluster "${CLUSTER2_NAME}"
}

function setup_kind_cluster() {
  IP_FAMILY="${1:-ipv4}"
  IMAGE="${2:-kindest/node:v1.17.0}"
  NAME="${3:-istio-testing}"
  CONFIG="${4:-}"
  # Delete any previous e2e KinD cluster
  echo "Deleting previous KinD cluster with name=${NAME}"
  if ! (kind delete cluster --name="${NAME}" -v9) > /dev/null; then
    echo "No existing kind cluster with name ${NAME}. Continue..."
  fi

  # explicitly disable shellcheck since we actually want $NAME to expand now
  # shellcheck disable=SC2064
  trap "cleanup_kind_cluster ${NAME}" EXIT

  # If config not explicitly set, then use defaults
  if [[ -z "${CONFIG}" ]]; then
    # Different Kubernetes versions need different patches
    K8S_VERSION=$(cut -d ":" -f 2 <<< "${IMAGE}")
    if [[ -n "${IMAGE}" && "${K8S_VERSION}" < "v1.13" ]]; then
      # Kubernetes 1.12
      CONFIG=./prow/config/trustworthy-jwt-12.yaml
    elif [[ -n "${IMAGE}" && "${K8S_VERSION}" < "v1.15" ]]; then
      # Kubernetes 1.13, 1.14
      CONFIG=./prow/config/trustworthy-jwt-13-14.yaml
    else
      # Kubernetes 1.15+
      CONFIG=./prow/config/trustworthy-jwt.yaml
    fi
      # Configure the cluster IP Family only for default configs
    if [ "${IP_FAMILY}" = "ipv6" ]; then
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

# Sets up 2 kind clusters (named $CLUSTER1_NAME, $CLUSTER2_NAME) configured for direct pod-to-pod traffic across
# clusters.
function setup_kind_multicluster_single_network() {
  IMAGE="${1}"
  # Create the kind configuration files for the 2 clusters
  CLUSTER1_YAML="${ARTIFACTS}/config-${CLUSTER1_NAME}.yaml"
  CLUSTER2_YAML="${ARTIFACTS}/config-${CLUSTER2_NAME}.yaml"
  cat <<EOF > "${CLUSTER1_YAML}"
    kind: Cluster
    apiVersion: kind.sigs.k8s.io/v1alpha3
    networking:
      podSubnet: 10.10.0.0/16
      serviceSubnet: 10.255.10.0/24
EOF
  cat <<EOF > "${CLUSTER2_YAML}"
    kind: Cluster
    apiVersion: kind.sigs.k8s.io/v1alpha3
    networking:
      podSubnet: 10.20.0.0/16
      serviceSubnet: 10.255.20.0/24
EOF

  CLUSTERREG_DIR="$(mktemp -d)"
  export CLUSTER1_KUBECONFIG="${CLUSTERREG_DIR}/${CLUSTER1_NAME}"
  export CLUSTER2_KUBECONFIG="${CLUSTERREG_DIR}/${CLUSTER2_NAME}"

  # Trap replaces any previous trap's, so we need to explicitly cleanup both clusters here
  trap cleanup_kind_multicluster EXIT

  # Create two clusters. Explicitly delcare the subnet so we can connect the two later
  # TODO: add IPv6
  KUBECONFIG="${CLUSTER1_KUBECONFIG}" setup_kind_cluster "ipv4" "${IMAGE}" "${CLUSTER1_NAME}" "${CLUSTER1_YAML}"
  KUBECONFIG="${CLUSTER2_KUBECONFIG}" setup_kind_cluster "ipv4" "${IMAGE}" "${CLUSTER2_NAME}" "${CLUSTER2_YAML}"

  # Replace with --internal which allows cross-cluster api server access
  kind get kubeconfig --name "${CLUSTER1_NAME}" --internal > "${CLUSTER1_KUBECONFIG}"
  kind get kubeconfig --name "${CLUSTER2_NAME}" --internal > "${CLUSTER2_KUBECONFIG}"

  # Set up routing rules for inter-cluster direct pod to pod communication
  CLUSTER1_NODE="${CLUSTER1_NAME}-control-plane"
  CLUSTER2_NODE="${CLUSTER2_NAME}-control-plane"
  CLUSTER1_DOCKER_IP=$(docker inspect -f "{{ .NetworkSettings.IPAddress }}" "${CLUSTER1_NODE}")
  CLUSTER2_DOCKER_IP=$(docker inspect -f "{{ .NetworkSettings.IPAddress }}" "${CLUSTER2_NODE}")
  CLUSTER1_POD_CIDR=$(KUBECONFIG="${CLUSTER1_KUBECONFIG}" kubectl get node -ojsonpath='{.items[0].spec.podCIDR}')
  CLUSTER2_POD_CIDR=$(KUBECONFIG="${CLUSTER2_KUBECONFIG}" kubectl get node -ojsonpath='{.items[0].spec.podCIDR}')
  docker exec "${CLUSTER1_NODE}" ip route add "${CLUSTER2_POD_CIDR}" via "${CLUSTER2_DOCKER_IP}"
  docker exec "${CLUSTER2_NODE}" ip route add "${CLUSTER1_POD_CIDR}" via "${CLUSTER1_DOCKER_IP}"
}

function cni_run_daemon_kind() {
  echo 'Run the CNI daemon set'
  ISTIO_CNI_HUB=${ISTIO_CNI_HUB:-gcr.io/istio-testing}
  ISTIO_CNI_TAG=${ISTIO_CNI_TAG:-latest}

  # TODO: this should not be pulling from external charts, instead the tests should checkout the CNI repo
  chartdir=$(mktemp -d)
  helm init --client-only
  helm repo add istio.io https://gcsweb.istio.io/gcs/istio-prerelease/daily-build/master-latest-daily/charts/
  helm fetch --devel --untar --untardir "${chartdir}" istio.io/istio-cni

  helm template --values "${chartdir}"/istio-cni/values.yaml --name=istio-cni --namespace=kube-system --set "excludeNamespaces={}" \
    --set-string hub="${ISTIO_CNI_HUB}" --set-string tag="${ISTIO_CNI_TAG}" --set-string pullPolicy=IfNotPresent --set logLevel="${CNI_LOGLVL:-debug}"  "${chartdir}"/istio-cni >  "${chartdir}"/istio-cni_install.yaml

  kubectl apply -f  "${chartdir}"/istio-cni_install.yaml
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
