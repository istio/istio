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

  KUBECONFIG="$(kind get kubeconfig-path --name="istio-testing")"
  export KUBECONFIG

cat <<EOF > metallb-config.yaml
apiVersion: v1
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
      - 172.17.255.1-172.17.255.250
EOF

  kubectl apply -f https://raw.githubusercontent.com/google/metallb/v0.8.1/manifests/metallb.yaml
  kubectl apply -f ./metallb-config.yaml
}

function setup_docker() {
  HUB=istio-testing TAG=latest make -f Makefile.core.mk controller docker
  kind --loglevel debug --name istio-testing load docker-image istio-testing/operator:istio-testing
}

setup_kind_cluster
setup_docker

mkdir -p "${ARTIFACTS}/out"

ISTIO_DIR="${GOPATH}/src/istio.io/istio"

# Create a clone of the Istio repository
if [[ ! -d "${ISTIO_DIR}" ]]
then
  git clone https://github.com/sdake/istio.git "${ISTIO_DIR}"
fi

# Create an operator manifest from the default control plane configuration

operator_manifest_files=( "deploy/namespace.yaml" "deploy/crds/istio_v1alpha2_istiocontrolplane_crd.yaml" "deploy/crds/istio_v1alpha2_istiocontrolplane_cr.yaml" "deploy/service_account.yaml" "deploy/clusterrole.yaml" "deploy/clusterrole_binding.yaml" "deploy/service.yaml" "deploy/operator.yaml" )

# Generate the main manifest
rm -f "${ISTIO_DIR}"/install/kubernetes/istio-operator.yaml
for manifest_file in "${operator_manifest_files[@]}"
do
	cat "${manifest_file}" >> "${ISTIO_DIR}"/install/kubernetes/istio-operator.yaml
	echo "---" >> "${ISTIO_DIR}"/install/kubernetes/istio-operator.yaml
done

kubectl get pods --all-namespaces -o wide

pushd "${ISTIO_DIR}" || exit
  make istioctl

  HUB=gcr.io/istio-testing TAG=latest E2E_ARGS="--use_operator --test_logs_path=${ARTIFACTS}" make e2e_simple_run
popd
