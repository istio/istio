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

export ARTIFACTS="${ARTIFACTS:-$(mktemp -d)}"

function cleanup_kind_cluster() {
    kind export logs --name istio-testing "${ARTIFACTS}/kind"
    kind delete cluster --name=istio-testing
}

function setup_kind_cluster() {
  # Delete any previous e2e KinD cluster
  echo "Deleting previous KinD cluster with name=istio-testing"
  if ! (kind delete cluster --name=istio-testing) > /dev/null; then
    echo "No existing kind cluster with name istio-testing. Continue..."
  fi

  trap cleanup_kind_cluster EXIT

  # Create KinD cluster
  if ! (kind create cluster --name=istio-testing --loglevel debug --retain --wait 30s); then
    echo "Could not setup KinD environment. Something wrong with KinD setup. Exporting logs."
    exit 1
  fi

  kubectl cluster-info
}

function setup_docker() {
  HUB=istio-testing TAG=istio-testing make -f Makefile.core.mk docker
  kind --loglevel debug --name istio-testing load docker-image istio-testing/install-cni:istio-testing
}

setup_kind_cluster
setup_docker

mkdir -p "${ARTIFACTS}/out"
helm template --name=istio-cni --namespace=kube-system  --set hub=istio-testing --set tag=istio-testing --set "excludeNamespaces={}" --set pullPolicy=IfNotPresent --set logLevel=debug deployments/kubernetes/install/helm/istio-cni > "${ARTIFACTS}/out/istio-cni_install.yaml"
kubectl apply -f "${ARTIFACTS}/out/istio-cni_install.yaml"
kubectl get pods --all-namespaces -o wide

ISTIO_DIR="${GOPATH}/src/istio.io/istio"
if [[ ! -d "${ISTIO_DIR}" ]]
then
  git clone https://github.com/istio/istio.git "${ISTIO_DIR}"
fi
pushd "${ISTIO_DIR}" || exit
  make istioctl

  HUB=gcr.io/istio-testing TAG=latest ENABLE_ISTIO_CNI=true E2E_ARGS="--kube_inject_configmap=istio-sidecar-injector --test_logs_path=${ARTIFACTS}" make test/local/auth/e2e_simple
popd
