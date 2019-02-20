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


#######################################
#                                     #
#         e2e-simpleTest-cni          #
#                                     #
#######################################

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

echo 'Running e2e_simple test with rbac, auth Tests and CNI enabled'

echo 'Run the CNI deamon set'
CNI_HUB=${CNI_HUB:-gcr.io/istio-release}
CNI_TAG=${CNI_TAG:-master-latest-daily}


chartdir=$(pwd)/charts
mkdir ${chartdir}
helm init --client-only
helm repo add istio.io https://storage.googleapis.com/istio-release/releases/1.1.0-snapshot.6/charts
helm fetch --untar --untardir ${chartdir} istio.io/istio-cni

helm template --values ${chartdir}/istio-cni/values.yaml --name=istio-cni --namespace=istio-system --set "excludeNamespaces={}" --set hub=${CNI_HUB} --set tag=${CNI_TAG} --set pullPolicy=IfNotPresent --set logLevel=${CNI_LOGLVL:-debug}  ${chartdir}/istio-cni > istio-cni_install.yaml

kubectl apply -f istio-cni_install.yaml

export ENABLE_ISTIO_CNI=true
# cniBinDir setting is appropriate for GKE environments
# HUB and TAG for the cni image will be based on the defaults checked in the cni repo
#export EXTRA_HELM_SETTINGS="--set istio-cni.excludeNamespaces={} --set istio-cni.cniBinDir=/home/kubernetes/bin"
# only gke-e2e-test-latest is enabled for Networkpolicy and Calico
export RESOURCE_TYPE="gke-e2e-test-latest"
# TODO - When the inline kube inject code defaults to using the configmap this setting can be removed.
export E2E_ARGS+=" --kube_inject_configmap=istio-sidecar-injector"
./prow/e2e-suite.sh --auth_enable --single_test e2e_simple "$@"
