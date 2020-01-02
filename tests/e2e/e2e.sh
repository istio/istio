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
mkdir -p "${ARTIFACTS}/out"

HUB=istio-testing TAG=istio-testing make -f Makefile.core.mk controller docker

ISTIO_DIR="${GOPATH}/src/istio.io/istio"

# Create a clone of the Istio repository
if [[ ! -d "${ISTIO_DIR}" ]]
then
	git clone https://github.com/istio/istio.git "${ISTIO_DIR}"
fi

# Write out our personal HUB and TAG to the operator iamge to be consumed
cp deploy/operator.yaml "${ARTIFACTS}/out"
yq w "${ARTIFACTS}"/out/operator.yaml spec.template.spec.containers[*].image istio-testing/operator:istio-testing -i

# yq doesn't preserve yaml start and end of documents - so we must create those for a proper deployment
echo "---" > "${ARTIFACTS}"/out/deployment.yaml
cat "${ARTIFACTS}"/out/operator.yaml >> "${ARTIFACTS}"/out/deployment.yaml
echo "..." >> "${ARTIFACTS}"/out/deployment.yaml

# Create an operator manifest from the default control plane configuration
operator_manifest_files=( "deploy/namespace.yaml" "deploy/crds/istio_v1alpha2_istiocontrolplane_crd.yaml" "deploy/service_account.yaml" "deploy/clusterrole.yaml" "deploy/clusterrole_binding.yaml" "deploy/service.yaml" "${ARTIFACTS}/out/deployment.yaml" "deploy/crds/istio_v1alpha2_istiocontrolplane_cr.yaml" )

# Generate the main manifest
rm -f "${ISTIO_DIR}"/install/kubernetes/istio-operator.yaml
cat "${operator_manifest_files[@]}" >> "${ISTIO_DIR}"/install/kubernetes/istio-operator.yaml

# Setup kind cluster
pushd "${ISTIO_DIR}"
# shellcheck disable=SC1091
source "./prow/lib.sh"
setup_kind_cluster ""

# Load the operator image into kind
kind --loglevel debug --name istio-testing load docker-image istio-testing/operator:istio-testing

KUBECONFIG=$(kind get kubeconfig-path --name="istio-testing")
export KUBECONFIG

make istioctl
# TODO: HUB and TAG here are not accurate. Instead `make docker.all` should be run to buld the
# docker images, rather than pull them. Pulling the images could result in image set A and
# image set B being tested in the same operator PR e2e check. This would emerge as flakey e2e
# test code.
HUB="gcr.io/istio-testing" TAG="latest" E2E_ARGS="--use_operator --use_local_cluster=true --kube_inject_configmap=inject --test_logs_path=${ARTIFACTS}" make e2e_simple_noauth
popd