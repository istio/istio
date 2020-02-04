#!/bin/bash

# Copyright Istio Authors
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

if [ "${V}" == 1 ]; then
  set -x
fi


#
# User defined configuration
#

# Keep all generated artificates in a dedicated workspace
: "${WORKDIR:?WORKDIR must be set}"

# Each mesh should have a unique ID.
: "${MESH_ID:?MESH_ID not set}"

# Organization name to use when creating the root and intermediate certificates.
: "${ORG_NAME:?ORG_NAME not set}"

#
# derived configuration
#

# Resolve to an absolute path
WORKDIR=$(cd "$(dirname "${WORKDIR}")" && pwd)/$(basename "${WORKDIR}")

# Per-cluster IstioOperator are derived from this common base IstioOperator.
BASE_FILENAME="${WORKDIR}/base.yaml"

# Description of the mesh topology. Includes the list of clusters in the mesh.
MESH_TOPOLOGY_FILENAME="${WORKDIR}/topology.yaml"

# Store root and intermediate certs
CERTS_DIR="${WORKDIR}/certs"

#
# helper functions
#

# Create a the initial mesh topology file. This includes the list of
# clusters in the mesh along with per-cluster attributes, e.g. network.
create_mesh_topology() {
  if [ -f "${MESH_TOPOLOGY_FILENAME}" ]; then
    echo "${MESH_TOPOLOGY_FILENAME} already exists."
    return
  fi
  echo "creating ${MESH_TOPOLOGY_FILENAME} to describe the mesh topology"
    cat > "${MESH_TOPOLOGY_FILENAME}" << EOF
# auto-generated mesh topology file.

# Example topology description.
#
# mesh_id: ${MESH_ID}
# contexts:
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
contexts:
# add your clusters here
EOF
}

# Create an offline root certificate. This root signs the
# intermediate certificates for each cluster.
create_offline_root_ca(){
  if [[ ! -d "${CERTS_DIR}" ]]; then
    mkdir "${CERTS_DIR}"
  fi

  if [[ -f "${CERTS_DIR}/root-key.pem" ]]; then
    echo "existing root key found - skipping root cert generation"
    return
  fi

  echo "generating root certs for ${ORG_NAME}"

  pushd "${CERTS_DIR}" >/dev/null
  # TODO - use local copy from the release if it were available?
  curl -sO https://raw.githubusercontent.com/istio/istio/release-1.4/samples/certs/Makefile
  export ROOTCA_ORG="${ORG_NAME}"
  export CITADEL_ORG="${ORG_NAME}"
  make root-ca >/dev/null
  popd >/dev/null
}

create_base() {
  if [ -f "${BASE_FILENAME}" ]; then
    echo "${BASE_FILENAME} already exists."
    return
  fi
  cat << EOF > "${BASE_FILENAME}"
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    security:
      selfSigned: false
    global:
      mtls:
        enabled: true
EOF
}

# Prepare the environment for building a multi-cluster mesh. This creates the
# mesh's nitial configuration and root certificate
prep_mesh() {
  create_mesh_topology
  create_base
  create_offline_root_ca

  echo "Success!"
  echo ""
  echo "    Add clusters to ${MESH_TOPOLOGY_FILENAME} and run '${0} apply' to build the mesh"
  echo ""
}

# Create an intermediate certificate with a common root for each
# cluster in the mesh.
apply_intermediate_ca() {
  echo "creating and installing intermediate certs for ${CONTEXT} in the ${ORG_NAME} org"
  local CONTEXT="${1}"
  kc() { kubectl --context "${CONTEXT}" "$@"; }

  pushd "${CERTS_DIR}"

  make "intermediate-${CONTEXT}-certs"
  cd "${CERTS_DIR}/intermediate-${CONTEXT}"

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
  local CONTEXT="${1}"
  ic() { istioctl --context "${CONTEXT}" "$@"; }

  # TODO remove the hub/tag or make it optional and parameterize it.
  ic x multicluster generate -f "${MESH_TOPOLOGY_FILENAME}" --from "${BASE_FILENAME}" > "${WORKDIR}/istio-${CONTEXT}.yaml"
  ic manifest apply -f "${WORKDIR}/istio-${CONTEXT}.yaml"
}

# TODO(ayj) - remove when the istioctl supports the --wait-for-gateway option
wait_for_gateway() {
  local CONTEXT="${1}"
  kc() { kubectl --context "${CONTEXT}" "$@"; }

  while true; do
    IP=$(kc -n istio-system get svc istio-ingressgateway -o "jsonpath"='{.status.loadBalancer.ingress[0].ip}')
    if [ "$IP" != '' ]; then
      echo "Ingress gateway external IP for cluster ${CONTEXT} is ready: ${IP}"
      return 0
    else
      echo "Wait for ingress gateway external IP for cluster ${CONTEXT} is ready."
    fi
    sleep 1
  done
}

create_passthrough_gateway() {
  local CONTEXT="${1}"
  kc() { kubectl --context "${CONTEXT}" "$@"; }

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

mesh_contexts() {
  sed -n 's/^  \([^ ]\+\):$/\1/p' "${MESH_TOPOLOGY_FILENAME}" | tr '\n' ' '
}

# apply the desired configuration to create the multi-cluster mesh.
apply() {
  echo "applying configuration to build the mesh"

  # Step 1) Install the intermediate certificate and Istio control
  # plane in each cluster
  for CONTEXT in $(mesh_contexts); do
      apply_intermediate_ca "${CONTEXT}"
      apply_istio_control_plane "${CONTEXT}"
  done

  # Step 2) The ingress gateway's external IP is required for
  # cross-network traffic. Wait for the external IPs to be
  # allocated before proceeding.
  for CONTEXT in $(mesh_contexts); do
    wait_for_gateway "${CONTEXT}"
  done

  # Step 3) re-generate and apply configuration. The updated
  # configuration includes the allocated external IPs from step (2).
  for CONTEXT in $(mesh_contexts); do
      apply_istio_control_plane "${CONTEXT}"

      # required for multi-network mesh.
      create_passthrough_gateway "${CONTEXT}"
  done

  # Step 4) Synchronize each cluster's service registry with the
  # other clusters in the mesh. This is required for cross-cluster
  # load balancing.
  istioctl x multicluster apply -f "${MESH_TOPOLOGY_FILENAME}"
}

# Teardown the mesh. Delete all of the in-cluster resources that
# may have been installed during setup.
teardown() {
  echo "Tearing down the mesh"

  for CONTEXT in $(mesh_contexts); do
    kc() { kubectl --context "${CONTEXT}" "$@"; }

    echo "Tearing down control cluster ${CONTEXT}"

    # wait for galley to die so it doesn't re-create the webhook config.
    kc delete namespace istio-system  --wait --ignore-not-found || true
    kc delete mutatingwebhookconfiguration -l operator.istio.io/managed=Reconcile --ignore-not-found
    kc delete validatingwebhookconfiguration -l operator.istio.io/managed=Reconcile --ignore-not-found

    # remove cluster scoped resources
    kc delete clusterrole -l operator.istio.io/managed=Reconcile --ignore-not-found
    kc delete clusterrolebinding -l operator.istio.io/managed=Reconcile --ignore-not-found
    kc delete crd -l operator.istio.io/managed=Reconcile --ignore-not-found
  done
}

describe() {
  istioctl x mc describe -f "${MESH_TOPOLOGY_FILENAME}"
}

check_prerequisties() {
  if [[ ! -d "${WORKDIR}" ]]; then
    echo "error: workspace directory '${WORKDIR}' not found. Run 'mkdir workspace && export WORKDIR=\$(pwd)/workspace' "
    exit 1
  fi
}

usage() {
  echo "Usage: $0 prep-mesh | apply | teardown

prep-mesh
  Prepare the workspace and files to build mesh. This includes the root key and
  cert, base IstioOperator configuration, and initial empty mesh topology
  file.

apply
  Apply the desired Istio control plane state and mesh topology to the
  clusters. This installs the Istio control plane in each cluster and registers
  each control plane with remote kube-apiservers for service and workload
  discovery.

teardown
  Remove the Istio control plane from all clusters.
"
}

check_prerequisties

case $1 in
  prep-mesh)
    prep_mesh
    ;;

  apply)
    apply
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
