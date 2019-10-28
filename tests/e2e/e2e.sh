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
HUB="gcr.io/istio-testing"
TAG="latest"
ROOT=$(cd ../../../; pwd)


function setup_docker() {
  HUB=istio-testing TAG=1.5-dev make controller docker
  kind --loglevel debug --name istio-testing load docker-image istio-testing/operator:1.5-dev
}



mkdir -p "${ARTIFACTS}/out"

ISTIO_DIR="${ROOT}/src/istio.io/istio"

# Create a clone of the Istio repository
if [[ ! -d "${ISTIO_DIR}" ]]
then
  git clone https://github.com/istio/istio.git "${ISTIO_DIR}"
fi

# Create an operator manifest from the default control plane configuration
cd "${ROOT}/src/istio.io/operator"
operator_manifest_files=( "deploy/namespace.yaml" "deploy/crds/istio_v1alpha2_istiocontrolplane_crd.yaml" "deploy/service_account.yaml" "deploy/clusterrole.yaml" "deploy/clusterrole_binding.yaml" "deploy/service.yaml" "tests/e2e/testdata/operator.yaml" "deploy/crds/istio_v1alpha2_istiocontrolplane_cr.yaml" )

# Generate the main manifest
rm -f "${ISTIO_DIR}"/install/kubernetes/istio-operator.yaml
for manifest_file in "${operator_manifest_files[@]}"
do
	cat "${manifest_file}" >> "${ISTIO_DIR}"/install/kubernetes/istio-operator.yaml
done


#kind cluster setup
pushd "${ISTIO_DIR}"
# shellcheck disable=SC1091
source "./prow/lib.sh"
setup_kind_cluster ""
popd

KUBECONFIG=$(kind get kubeconfig-path --name="istio-testing")
export KUBECONFIG
setup_docker

pushd "${ISTIO_DIR}" || exit
  make istioctl
  HUB="${HUB}" TAG="${TAG}" E2E_ARGS="--use_operator --use_local_cluster=true --test_logs_path=${ARTIFACTS}" make e2e_simple_noauth_run
popd
