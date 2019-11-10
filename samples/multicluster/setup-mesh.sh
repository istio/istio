#!/bin/bash

# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit

#
# User defined configuration
#

# Keep all generated artificates in a dedicated workspace
: "${WORKDIR:?WORKDIR not set}"

# Each mesh should have a unique ID.
: "${MESH_ID:?MESH_ID not set}"

# Organization name to use when creating the root and intermediate certificates.
: "${ORG_NAME:?ORG_NAME not set}"

# kubeconfig and context for the three test clusters
: "${CLUSTER0_CONTEXT:?CLUSTER0_CONTEXT not set}"
: "${CLUSTER0_KUBECONFIG:?CLUSTER0_KUBECONFIG not set}"
: "${CLUSTER1_CONTEXT:?CLUSTER1_CONTEXT not set}"
: "${CLUSTER1_KUBECONFIG:?CLUSTER1_KUBECONFIG not set}"
: "${CLUSTER2_CONTEXT:?CLUSTER2_CONTEXT not set}"
: "${CLUSTER2_KUBECONFIG:?CLUSTER2_KUBECONFIG not set}"

#
# derived configuration
#

# Resolve to an absolute path
WORKDIR=$(readlink -f ${WORKDIR})

# kubeconfig containing the three test clusters
MERGED_KUBECONFIG=${WORKDIR}/"mesh.kubeconfig"

# Per-cluster IstioControlPlane are derived from this common base IstioControlPlane.
BASE_FILENAME=${WORKDIR}/base.yaml

# Description of the mesh topology. Includes the list of clusters in the mesh.
MESH_TOPOLOGY_FILENAME=${WORKDIR}/topology.yaml

# Store root and intermediate certs
CERTS_DIR=${WORKDIR}/certs

#
# helper functions
#

# track existing contexts that were already merged. A random suffix
# is added to non-unique contexts.
declare -A EXISTING_CONTEXTS

# accumulate kubeconfigs that will be later merged.
export KUBECONFIG_LIST=""

# extract the minified kubeconfig for a single cluster as specified
# by the kubeconfig path and context name.
extract_kubeconfig() {
  local KUBECONFIG=${1}
  local CONTEXT=${2}
  local MINIFIED_KUBECONFIG

  MINIFIED_KUBECONFIG=$(mktemp ${TMP_KUBECONFIG_DIR}/XXXXXX)
  kubectl --kubeconfig ${KUBECONFIG} --context ${CONTEXT} \
    config view --minify --raw > ${MINIFIED_KUBECONFIG}

  if [[ ${EXISTING_CONTEXTS[${CONTEXT}]+_} ]]; then
    local CONTEXT_RENAMED=${CONTEXT}"-${RANDOM}"

    echo "${CONTEXT} is not unique in the merged kubeconfig. Renaming to ${CONTEXT_RENAMED} " > /dev/stderr
    kubectl --kubeconfig ${MINIFIED_KUBECONFIG} \
      config rename-context ${CONTEXT} ${CONTEXT_RENAMED} > /dev/null
    CONTEXT=${CONTEXT_RENAMED}
  fi

  EXISTING_CONTEXTS["${CONTEXT}"]=y
  KUBECONFIG_LIST=${KUBECONFIG_LIST}:${MINIFIED_KUBECONFIG}
}

# Prepare a kubeconfig with clusters in the mesh.
prepare_kubeconfig() {
  # create a local working directory to manage kubeconfig files.
  export TMP_KUBECONFIG_DIR=$(mktemp -d ./kubeconfig_workdir_XXXXXX)
  cleanup() {
    rm -rf ${TMP_KUBECONFIG_DIR}
  }
  trap cleanup EXIT

  extract_kubeconfig ${CLUSTER0_KUBECONFIG} ${CLUSTER0_CONTEXT}
  extract_kubeconfig ${CLUSTER1_KUBECONFIG} ${CLUSTER1_CONTEXT}
  extract_kubeconfig ${CLUSTER2_KUBECONFIG} ${CLUSTER2_CONTEXT}

  KUBECONFIG=${KUBECONFIG_LIST} kubectl config view --raw --flatten > ${MERGED_KUBECONFIG}

  echo ""
  echo "Success! Run the following command to use as the default kubeconfig in the current shell"
  echo
  echo "    export KUBECONFIG=${MERGED_KUBECONFIG}"
  echo
}

# Create a IstioControlPlane that will be the base configuration
# for all cluster's control planes.
create_base_yaml() {
  echo "creating ${BASE_FILENAME} for the common base control plane configuration"
  cat > ${BASE_FILENAME} << EOF
apiVersion: install.istio.io/v1alpha2
kind: IstioControlPlane
spec:
  values:
    security:
      selfSigned: false
    global:
      mtls:
        enabled: true
EOF
}

# Create a the initial mesh topology file. This includes the list of
# clusters in the mesh along with per-cluster attributes, e.g. network.
create_mesh_topology() {
  echo "creating ${MESH_TOPOLOGY_FILENAME} to describe the mesh topology"
  cat > ${MESH_TOPOLOGY_FILENAME} << EOF
# auto-generated mesh topology file.

# Example topology description.
#
# mesh_id: ${MESH_ID}
# clusters:
#   # 'cluster0' is the cluster's context as defined by the
#   # current kubeconfig file
#   cluster0:
#     # 'network' is a user chosen name of the cluster's
#     # network, e.g. name of the VPC the cluster is running in.
#     network: network-0
#
#     # name of the k8s serviceaccount other clusters will use
#     # when reading services from this cluster.
#     serviceAccountReader: istio-reader-service-account
#
#   # multiple clusters can be added.
#   cluster1:
#     network: network-1
#     serviceAccountReader: istio-reader-service-account
#

mesh_id: ${MESH_ID}
clusters:
# add your clusters here
EOF
}

# Create an offline root certificate. This root signs the
# intermediate certificates for each cluster.
create_offline_root_ca(){
  if [[ ! -d ${CERTS_DIR} ]]; then
    mkdir ${CERTS_DIR}
  fi

  if [[ -f "${CERTS_DIR}/root-key.pem" ]]; then
    echo "existing root key found - skipping root cert generation"
    return
  fi

  echo "generating root certs for ${ORG_NAME}"

  pushd ${CERTS_DIR} >/dev/null
  # TODO - use local copy from the release if it were available?
  curl -sO https://raw.githubusercontent.com/istio/istio/release-1.4/samples/certs/Makefile
  export ROOTCA_ORG=${ORG_NAME}
  export CITADEL_ORG=${ORG_NAME}
  make root-ca >/dev/null
  popd >/dev/null
}

# Prepare the environment for building a multi-cluster mesh. This
# creates the mesh's nitial configuration and root certificate
prepare_mesh() {
  create_base_yaml
  create_mesh_topology
  create_offline_root_ca

  echo "Success!"
  echo ""
  echo "    Add clusters to ${MESH_TOPOLOGY_FILENAME} and run '${0} apply' to update the mesh"
  echo ""
}

# TODO(ayj) - remove once istioctl installs these
apply_missing_service_account() {
  local CONTEXT=${1}
  kc() { kubectl --context ${CONTEXT} $@;  }

  kc create clusterrole istio-reader-istio-system \
    --verb=get,list,watch --resource=nodes,pods,endpoints,services,replicasets.apps,replicationcontrollers \
    --dry-run -o yaml | kc apply -f -
  kc create serviceaccount istio-reader-service-account -n istio-system \
    --dry-run -o yaml | kc apply -f -
  kc create clusterrolebinding istio-reader-istio-system \
    --clusterrole=istio-reader-istio-system --serviceaccount=istio-system:istio-reader-service-account \
    --dry-run -o yaml | kc apply -f -
}

# Create an intermediate certificate with a common root for each
# cluster in the mesh.
apply_intermediate_ca() {
  echo "creating and installing intermediate certs for ${CONTEXT} in the ${ORG_NAME} org"
  local CONTEXT=${1}
  kc() { kubectl --context ${CONTEXT} $@; }

  pushd ${CERTS_DIR}

  make intermediate-${CONTEXT}-certs
  cd ${CERTS_DIR}/intermediate-${CONTEXT}

  kc create namespace istio-system \
    --dry-run -o yaml | kc apply -f -
  kc create secret generic cacerts -n istio-system \
    --from-file=ca-cert.pem \
    --from-file=ca-key.pem \
    --from-file=root-cert.pem \
    --from-file=cert-chain.pem \
    --dry-run -o yaml| kc apply -f -

  popd
}

apply_istio_control_plane() {
  local CONTEXT=${1}
  ic() { istioctl --context ${CONTEXT} $@; }

  # TODO remove the hub/tag or make it optional and parameterize it.
  ic x multicluster generate -f ${MESH_TOPOLOGY_FILENAME} --from ${BASE_FILENAME} > ${WORKDIR}/istio-${CONTEXT}.yaml
  ic manifest apply -f ${WORKDIR}/istio-${CONTEXT}.yaml --set hub=docker/istio.io --set tag=1.4.0-beta.3
}

# TODO(ayj) - remove when the istioctl supports the --wait-for-gateway option
wait_for_gateway() {
  local CONTEXT=${1}
  kc() { kubectl --context ${CONTEXT} $@; }

  while true; do
    IP=(kc istio-system get svc istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    if [[ $? -eq 0 ]]; then
      echo "Ingress gateway external IP for cluster ${CONTEXT} is ready: ${IP}"
      return 0
    fi
    sleep 1
  done
}

create_passthrough_gateway() {
  local CONTEXT=${1}
  kc() { kubectl --context ${CONTEXT} $@; }

  kc apply -f - <<EOF
apiVersion: networking.istio.io/v1alpha3
kind: Gateway
metadata:
  name: cluster-aware-gateway
  namespace: istio-system
spec:
  selector:
    istio: ingressgateway
  servers:
  - port:
      number: 443
      name: tls
      protocol: TLS
    tls:
      mode: AUTO_PASSTHROUGH
    hosts:
    - "*.local"
EOF
}

# apply the desired configuration to create the multi-cluster mesh.
apply() {
  echo "applying configuration to build the mesh"

  # Step 1) Install the intermediate certificate and Istio control
  # plane in each cluster
  for CONTEXT in $(kubectl config get-contexts -o name); do
      apply_intermediate_ca ${CONTEXT}
      apply_missing_service_account ${CONTEXT}
      apply_istio_control_plane ${CONTEXT}
  done

  # Step 2) The ingress gateway's external IP is required for
  # cross-network traffic. Wait for the external IPs to be
  # allocated before proceeding.
  for CONTEXT in $(kubectl config get-contexts -o name); do
    wait_for_gateway ${CONTEXT}
  done

  # Step 3) re-generate and apply configuration. The updated
  # configuration includes the allocated external IPs from step (2).
  for CONTEXT in $(kubectl config get-contexts -o name); do
      apply_istio_control_plane ${CONTEXT}

      # required for multi-network mesh.
      create_passthrough_gateway ${CONTEXT}
  done

  # Step 4) Synchronize each cluster's service registry with the
  # other clusters in the mesh. This is required for cross-cluster
  # load balancing.
  istioctl x multicluster apply -f ${MESH_TOPOLOGY_FILENAME}
}

# Teardown the mesh. Delete all of the in-cluster resources that
# may have been installed during setup.
teardown() {
  echo "Tearing down the mesh"

  for CONTEXT in $(kubectl config get-contexts -o name); do
    kc() { kubectl --context ${CONTEXT} $@; }

    # wait for galley to die so it doesn't re-create the webhook config.
    kc delete namespace istio-system  --wait --ignore-not-found >/dev/null || true
    kc delete mutatingwebhookconfiguration -l operator.istio.io/managed=Reconcile --ignore-not-found
    kc delete validatingwebhookconfiguration -l operator.istio.io/managed=Reconcile --ignore-not-found

    # remove cluster scoped resources
    kc delete clusterrole -l operator.istio.io/managed=Reconcile --ignore-not-found
    kc delete clusterrolebinding -l operator.istio.io/managed=Reconcile --ignore-not-found
    kc delete crd -l operator.istio.io/managed=Reconcile --ignore-not-found

    # remove the bookinfo app in the default namespace
    kc delete -f ../bookinfo/platform/kube/bookinfo.yaml --wait --ignore-not-found
  done
}

install_bookinfo() {
  echo "Installing bookinfo in each cluster"

  for CONTEXT in $(kubectl config get-contexts -o name); do
    kc() { kubectl --context ${CONTEXT} $@; }

    # enable automatic sidecar injection on the default namespace
    kc label namespace default istio-injection=enabled --overwrite

    # install the bookinfo service and expose it through the ingress gateway
    kc apply -f ../bookinfo/platform/kube/bookinfo.yaml
    kc apply -f ../bookinfo/networking/bookinfo-gateway.yaml
  done
}

describe() {
  istioctl x mc describe -f ${MESH_TOPOLOGY_FILENAME}
}

check_prerequisties() {
  if [[ ! -d ${WORKDIR} ]]; then
    echo "error: workspace directory '${WORKDIR}' does not exist"
    exit 1
  fi
}

usage() {
  echo "Usage: prepare-kubeconfig  | prepare-mesh | apply | bookinfo

prepare-kubeconfig
  Create a merged kubeconfig containing only clusters in the mesh.

prepare-mesh
  Prepare the workspace with the initial files and root certificates
  to build a mesh.

apply
  Apply the desired control plane and multicluster state to the
  clusters. This installs the Istio control plane in each cluster
  and registers each control plane with remote kube-apiservers for
  service and workload discovery.

install-bookinfo
  Install the sample bookinfo application in each cluster.

teardown
  Remove the Istio control plane and sample bookinfo app from all clusters.
"
}

check_prerequisties

case $1 in
  prepare-kubeconfig)
    prepare_kubeconfig
    ;;

  prepare-mesh)
    prepare_mesh
    ;;

  apply)
    apply
    ;;

  install-bookinfo)
    install_bookinfo
    ;;

  describe)
    describe
    ;;

  teardown)
    teardown
    ;;
  *)
    usage
    exit 127
esac
