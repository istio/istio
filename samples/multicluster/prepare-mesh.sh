#!/bin/bash

set -e

: "${WORKDIR:?WORKDIR not set}"
: "${MESH_ID:?MESH_ID not set}"
: "${ORG_NAME:?ORG_NAME not set}"

BASE_FILENAME=${WORKDIR}/base.yaml
MESH_TOPOLOGY_FILENAME=${WORKDIR}/topology.yaml
CERTS_DIR=${WORKDIR}/certs
UPDATE_CLUSTER_SCRIPT="update-cluster.sh"


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


create_base_topology() {
  echo "creating ${MESH_TOPOLOGY_FILENAME} to describe the mesh topology"
  cat > ${MESH_TOPOLOGY_FILENAME} << EOF
# auto-generated mesh topology file.
#
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

create_base_yaml
create_base_topology
create_offline_root_ca

echo "Success!"
echo ""
echo "    Add clusters to ${MESH_TOPOLOGY_FILENAME} and run ${UPDATE_CLUSTER_SCRIPT} to update the mesh"
echo ""
