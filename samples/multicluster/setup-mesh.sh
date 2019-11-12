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
: "${WORKDIR:-${WORKDIR_DEFAULT}}"

# Each mesh should have a unique ID.
: "${MESH_ID:?MESH_ID not set}"

# Organization name to use when creating the root and intermediate certificates.
: "${ORG_NAME:?ORG_NAME not set}"

#
# derived configuration
#

# Resolve to an absolute path
WORKDIR=$(cd "$(dirname "${WORKDIR}")" && pwd)/$(basename "${WORKDIR}")

# kubeconfig containing the three test clusters
MERGED_KUBECONFIG="${WORKDIR}/mesh.kubeconfig"

# Per-cluster IstioControlPlane are derived from this common base IstioControlPlane.
BASE_FILENAME="${WORKDIR}/base.yaml"

# Description of the mesh topology. Includes the list of clusters in the mesh.
MESH_TOPOLOGY_FILENAME="${WORKDIR}/topology.yaml"

# intermediate list of clusters
CLUSTER_LIST="${WORKDIR}/cluster_list"

# Store root and intermediate certs
CERTS_DIR="${WORKDIR}/certs"

#
# helper functions
#

generate_topology() {
  # create a local working directory to manage kubeconfig files.
  TMP_KUBECONFIG_DIR="$(mktemp -d ./kubeconfig_workdir_XXXXXX)"
  export TMP_KUBECONFIG_DIR
  cleanup() {
    rm -rf "${TMP_KUBECONFIG_DIR}"
  }
  trap cleanup EXIT

  MESH_ID=$(grep -e "^mesh: \(.*\)$" "${CLUSTER_LIST}" |cut -d: -f2 | sed 's/ //')

  cat <<EOF > "${MESH_TOPOLOGY_FILENAME}"
# auto-generated - do not edit by hand
mesh_id: ${MESH_ID}
contexts:
EOF

  # pass 1) generate the topology.yaml file.
  while IFS=' ' read -r TYPE KUBECONFIG CONTEXT NETWORK; do
    [[ "${TYPE}" != "cluster:" ]] && continue

    echo "  ${CONTEXT}:" >> "${MESH_TOPOLOGY_FILENAME}"
    echo "    network: ${NETWORK}" >> "${MESH_TOPOLOGY_FILENAME}"
  done < "${CLUSTER_LIST}"

  # pass 2) re-generate a single merged kubeconfig for clusters in the mesh.
  local KUBECONFIG_LIST=""
  while IFS=' ' read -r TYPE KUBECONFIG CONTEXT NETWORK; do
    [[ "${TYPE}" != "cluster:" ]] && continue

    MINIFIED_KUBECONFIG=$(mktemp "${TMP_KUBECONFIG_DIR}"/XXXXXX)
    kubectl --kubeconfig "${KUBECONFIG}" --context "${CONTEXT}" config view --minify --raw > "${MINIFIED_KUBECONFIG}"
    KUBECONFIG_LIST="${KUBECONFIG_LIST}":"${MINIFIED_KUBECONFIG}"
  done  < "${CLUSTER_LIST}"
  KUBECONFIG="${KUBECONFIG_LIST}" kubectl config view --raw --flatten > "${MERGED_KUBECONFIG}"
}

add_cluster() {
  kubeconfig=${1}
  context=${2}
  network=${3}

  LINE="cluster: ${kubeconfig} ${context} ${network}"

  # The network or underlying kubeconfig contents may have changed for an
  # existing {kubeconfig,context} tuple. Remove the current entry so we can
  # re-add it properly in subsqeuent steps.
  if grep -q -e "cluster: ${kubeconfig} ${context}" "${CLUSTER_LIST}"; then
    sed -i "\|^cluster: ${kubeconfig} ${context} |d" "${CLUSTER_LIST}"
  fi

  # contexts must be unique within the mesh.
  CONTEXTS=${context}
  while IFS=' ' read -r TYPE KUBECONFIG CONTEXT NETWORK; do
    if [ "${CONTEXTS}" != "" ]; then
      CONTEXTS="${CONTEXTS}\n"
    fi
    CONTEXTS="${CONTEXTS}${CONTEXT}"
  done < "${CLUSTER_LIST}"

  ERROR=0
  while read -r COUNT CONTEXT; do
    if [ "${COUNT}" != 1 ]; then
      echo "Context ${CONTEXT} is not unique. Please rename with 'kubectl config rename-context' and try again"
      ERROR=1
    fi
  done < <(echo -e ${CONTEXTS} | sort | uniq -c)

  if [ "${ERROR}" == 1 ]; then
    exit 127
  fi

  echo "${LINE}" >> "${CLUSTER_LIST}"
  generate_topology
}

remove_cluster() {
  kubeconfig=${1}
  context=${2}
  network=${3}

  LINE="cluster: ${kubeconfig} ${context} ${network}"

  sed -i "\|^${LINE}|d" "${CLUSTER_LIST}"
  generate_topology
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
  cat << EOF > "${BASE_FILENAME}"
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

# Create the environment for building a multi-cluster mesh. This
# creates the mesh's nitial configuration and root certificate
create_mesh() {
  create_base
  create_offline_root_ca

  # create mesh configuration
  echo "mesh_id: ${MESH_ID}" > "${CLUSTER_LIST}"

  echo "    Success!"
  echo ""
  echo "    Use '${0} add-cluster <kubeconfig> <context> <network>' to add clusters"
  echo "    to the mesh configuration. Run '${0} apply' to apply the configuration to the live"
  echo "    clusters to form the mesh".
  echo ""
  echo "    Run the following command to use as the default kubeconfig in the current shell"
  echo
  echo "    export KUBECONFIG=${MERGED_KUBECONFIG}"
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
  ic manifest apply -f "${WORKDIR}/istio-${CONTEXT}.yaml" --set hub=docker.io/istio --set tag=1.4.0-beta.3
}

# TODO(ayj) - remove when the istioctl supports the --wait-for-gateway option
wait_for_gateway() {
  local CONTEXT="${1}"
  kc() { kubectl --context "${CONTEXT}" "$@"; }

  while true; do
    if IP=$(kc -n istio-system get svc istio-ingressgateway -o "jsonpath"='{.status.loadBalancer.ingress[0].ip}'); then
      echo "Ingress gateway external IP for cluster ${CONTEXT} is ready: ${IP}"
      return 0
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

# apply the desired configuration to create the multi-cluster mesh.
apply() {
  echo "applying configuration to build the mesh"

  # Step 1) Install the intermediate certificate and Istio control
  # plane in each cluster
  for CONTEXT in $(kubectl config get-contexts -o name); do
      apply_intermediate_ca "${CONTEXT}"
      apply_istio_control_plane "${CONTEXT}"
  done

  # Step 2) The ingress gateway's external IP is required for
  # cross-network traffic. Wait for the external IPs to be
  # allocated before proceeding.
  for CONTEXT in $(kubectl config get-contexts -o name); do
    wait_for_gateway "${CONTEXT}"
  done

  # Step 3) re-generate and apply configuration. The updated
  # configuration includes the allocated external IPs from step (2).
  for CONTEXT in $(kubectl config get-contexts -o name); do
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

  for CONTEXT in $(kubectl config get-contexts -o name); do
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
  echo "Usage: $0 create-mesh | add-cluster | remove-cluster | apply | teardown

create-mesh
  Create the workspace with the initial files and root certificates
  to build a mesh.

add-cluster <kubeconfig> <context> <network>
  Add a cluster to the mesh configuration. Use '${0} apply' to apply
  the configuration changes to the mesh.

remove-cluster <kubeconfig> <context> <network>
  Remove a cluster from the mesh configuration. Use '${0} apply' to
  apply the configuration changes to the mesh.

apply
  Apply the desired control plane and multicluster state to the
  clusters. This installs the Istio control plane in each cluster
  and registers each control plane with remote kube-apiservers for
  service and workload discovery.

teardown
  Remove the Istio control plane from all clusters.
"
}

check_prerequisties

case $1 in
  create-mesh)
    create_mesh
    ;;

  add-cluster)
    if [ "$#" -ne 4 ]; then
      usage
      exist 127
    fi

    kubeconfig=${2}
    context=${3}
    network=${4}

    add_cluster "${kubeconfig}" "${context}" "${network}"
    ;;

  remove-cluster)
    if [ "$#" -ne 4 ]; then
      usage
      exit 127
    fi

    kubeconfig=${2}
    context=${3}
    network=${4}

    remove_cluster "${kubeconfig}" "${context}" "${network}"
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
