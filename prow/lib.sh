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

function setup_and_export_git_sha() {
  if [[ -n "${CI:-}" ]]; then
    if [[ "${CI:-}" == 'bootstrap' ]]; then
      # TODO: Remove after update to pod-utils

      # Make sure we are in the right directory
      # Test harness will checkout code to directory $GOPATH/src/github.com/istio/istio
      # but we depend on being at path $GOPATH/src/istio.io/istio for imports
      if [[ ! $PWD = ${GOPATH}/src/istio.io/istio ]]; then
        mv "${GOPATH}/src/github.com/${REPO_OWNER:-istio}" "${GOPATH}/src/istio.io"
        export ROOT=${GOPATH}/src/istio.io/istio
        cd "${GOPATH}/src/istio.io/istio" || return
      fi

      # Set artifact dir based on checkout
      export ARTIFACTS_DIR="${ARTIFACTS_DIR:-${GOPATH}/src/istio.io/istio/_artifacts}"

    elif [[ "${CI:-}" == 'prow' ]]; then
      # Set artifact dir based on checkout
      export ARTIFACTS_DIR="${ARTIFACTS_DIR:-${ARTIFACTS}}"
    fi

    if [ -z "${PULL_PULL_SHA:-}" ]; then
      export GIT_SHA="${PULL_BASE_SHA}"
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
  gcloud auth configure-docker -q
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

  export ISTIOCTL_BIN="${GOPATH}/src/istio.io/istio/istio-${TAG}/bin/istioctl"
}

# Cleanup e2e resources.
function cleanup() {
  if [[ "${CLEAN_CLUSTERS}" == "True" ]]; then
    unsetup_clusters
  fi
  if [[ "${USE_MASON_RESOURCE}" == "True" ]]; then
    mason_cleanup
    cat "${FILE_LOG}"
  fi
}

# Set up a GKE cluster for testing.
function setup_e2e_cluster() {
  WD=$(dirname "$0")
  WD=$(cd "$WD" || exit; pwd)
  ROOT=$(dirname "$WD")

  # shellcheck source=prow/mason_lib.sh
  source "${ROOT}/prow/mason_lib.sh"
  # shellcheck source=prow/cluster_lib.sh
  source "${ROOT}/prow/cluster_lib.sh"

  trap cleanup EXIT

  if [[ "${USE_MASON_RESOURCE}" == "True" ]]; then
    INFO_PATH="$(mktemp /tmp/XXXXX.boskos.info)"
    FILE_LOG="$(mktemp /tmp/XXXXX.boskos.log)"
    OWNER=${OWNER:-"e2e"}
    E2E_ARGS+=("--mason_info=${INFO_PATH}")

    setup_and_export_git_sha

    get_resource "${RESOURCE_TYPE}" "${OWNER}" "${INFO_PATH}" "${FILE_LOG}"
  else
    export GIT_SHA="${GIT_SHA:-$TAG}"
  fi
  setup_cluster
}

function clone_cni() {
  # Clone the CNI repo so the CNI artifacts can be built.
  if [[ "$PWD" == "${GOPATH}/src/istio.io/istio" ]]; then
      TMP_DIR=$PWD
      cd ../ || return
      git clone -b master "https://github.com/istio/cni.git"
      cd "${TMP_DIR}" || return
  fi
}

function check_kind() {
  echo "Checking KinD is installed..."
  if ! kind --help > /dev/null; then
    echo "Looks like KinD is not installed."
    exit 1
  fi
}

function setup_kind_cluster() {
  # Installing KinD
  check_kind

  # Delete any previous e2e KinD cluster
  echo "Deleting previous KinD cluster with name=e2e-suite"
  if ! (kind delete cluster --name=e2e-suite) > /dev/null; then
    echo "No Found existing kind cluster with name e2e-suite. Continue..."
  fi

  # Create KinD cluster
  if ! (kind create cluster --name=e2e-suite); then
    echo "Could not setup KinD environment. Something wrong with KinD setup. Please check your setup and try again."
    exit 1
  fi
  KUBECONFIG="$(kind get kubeconfig-path --name="e2e-suite")"
  export KUBECONFIG
}

function cni_run_daemon() {

  echo 'Run the CNI daemon set'
  ISTIO_CNI_HUB=${ISTIO_CNI_HUB:-gcr.io/istio-release}
  ISTIO_CNI_TAG=${ISTIO_CNI_TAG:-master-latest-daily}

  chartdir=$(pwd)/charts
  mkdir "${chartdir}"
  helm init --client-only
  helm repo add istio.io https://gcsweb.istio.io/gcs/istio-prerelease/daily-build/release-1.1-latest-daily/charts/
  helm fetch --untar --untardir "${chartdir}" istio.io/istio-cni

  helm template --values "${chartdir}"/istio-cni/values.yaml --name=istio-cni --namespace=kube-system --set "excludeNamespaces={}" --set cniBinDir=/home/kubernetes/bin --set hub="${ISTIO_CNI_HUB}" --set tag="${ISTIO_CNI_TAG}" --set pullPolicy=IfNotPresent --set logLevel="${CNI_LOGLVL:-debug}"  "${chartdir}"/istio-cni > istio-cni_install.yaml

  kubectl apply -f istio-cni_install.yaml

}
