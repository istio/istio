#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. ${TESTS_DIR}/commonUtils.sh || { echo "Cannot load common utilities"; exit 1; }

K8CLI="kubectl"

# Create a kube namespace to isolate test
function create_namespace(){
    print_block_echo "Creating kube namespace"
    $K8CLI create namespace $NAMESPACE \
    || error_exit 'Failed to create namespace'
}

# Bring up control plane
function deploy_istio() {
    print_block_echo "Deploying ISTIO"
    $K8CLI -n $NAMESPACE create -f "${TESTS_DIR}/istio/controlplane.yaml" \
      || error_exit 'Failed to create control plane'
    retry -n 10 find_istio_endpoints \
      || error_exit 'Could not deploy istio'
}

function find_istio_endpoints() {
    local endpoints=($(${K8CLI} get endpoints -n ${NAMESPACE} \
      -o jsonpath='{.items[*].subsets[*].addresses[*].ip}'))
    echo ${endpoints[@]}
    [[ ${#endpoints[@]} -eq 2 ]] && return 0
    return 1
}

# Deploy the bookinfo microservices
function deploy_bookinfo(){
    print_block_echo "Deploying BookInfo to kube"
    $K8CLI -n $NAMESPACE create -f "${TESTS_DIR}/apps/bookinfo/bookinfo.yaml" \
      || error_exit 'Failed to deploy bookinfo'
    retry -n 10 find_ingress_gateway \
      || error_exit 'Could not deploy bookstore'
}

function find_ingress_gateway() {
    local ip="$(${K8CLI} get ingress gateway -n ${NAMESPACE} \
      -o jsonpath='{.status.loadBalancer.ingress[*].ip}')"
    echo ${ip[@]}
    [[ ${ip} =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$ ]] && return 0
    return 1
}

# Clean up all the things
function cleanup() {
    print_block_echo "Cleaning up ISTIO"
    $K8CLI -n $NAMESPACE delete -f "${TESTS_DIR}/istio/controlplane.yaml"
    print_block_echo "Cleaning up BookInfo"
    $K8CLI -n $NAMESPACE delete -f "${TESTS_DIR}/apps/bookinfo/bookinfo.yaml"
    print_block_echo "Deleting namespace"
    $K8CLI delete namespace $NAMESPACE
}

# Debug dump for failures
function dump_debug() {
    echo ""
    $K8CLI -n $NAMESPACE get pods
    $K8CLI -n $NAMESPACE get thirdpartyresources
    $K8CLI -n $NAMESPACE get thirdpartyresources -o json
    GATEWAY_PODNAME=$($K8CLI -n $NAMESPACE get pods | grep istio-ingress | awk '{print $1}')
    $K8CLI -n $NAMESPACE logs $GATEWAY_PODNAME
    PRODUCTPAGE_PODNAME=$($K8CLI -n $NAMESPACE get pods | grep productpage | awk '{print $1}')
    $K8CLI -n $NAMESPACE logs $PRODUCTPAGE_PODNAME -c productpage
    $K8CLI -n $NAMESPACE logs $PRODUCTPAGE_PODNAME -c proxy
    $K8CLI -n $NAMESPACE get istioconfig -o yaml
}
