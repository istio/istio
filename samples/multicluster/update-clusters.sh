#!/bin/bash

set -e

: "${WORKDIR:?WORKDIR not set}"
: "${ORG_NAME:?ORG_NAME not set}"

: "${CLUSTER0_CONTEXT:?CLUSTER0_CONTEXT not set}"
: "${CLUSTER1_CONTEXT:?CLUSTER1_CONTEXT not set}"
: "${CLUSTER2_CONTEXT:?CLUSTER2_CONTEXT not set}"

# use absolute paths
WORKDIR=$(readlink -f ${WORKDIR})

export KUBECONFIG=${WORKDIR}/mesh.kubeconfig
echo "using KUBECONFIG=${KUBECONFIG} for all kubectl and istioctl commands"

BASE_FILENAME=${WORKDIR}/base.yaml
MESH_TOPOLOGY_FILENAME=${WORKDIR}/topology.yaml
CERTS_DIR=${WORKDIR}/certs

DRY_RUN=false

create_and_install_intermediate_ca() {
  echo "creating and installing intermediate certs for ${CONTEXT} in the ${ORG_NAME} org"
  local CONTEXT=${1}
  kc() { kubectl --context ${CONTEXT} --dry-run=${DRY_RUN} $@; }

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

# TODO(ayj) - remove once istioctl installs these
apply_missing_service_account() {
  local CONTEXT=${1}
  kc() { kubectl --context ${CONTEXT} --dry-run=${DRY_RUN} $@; }

  kc create clusterrole istio-reader-istio-system \
    --verb=get,list,watch --resource=nodes,pods,endpoints,services,replicasets.apps,replicationcontrollers \
    --dry-run -o yaml | kc apply -f -

  kc create serviceaccount istio-reader-service-account -n istio-system \
    --dry-run -o yaml | kc apply -f -

  kc create clusterrolebinding istio-reader-istio-system \
    --clusterrole=istio-reader-istio-system --serviceaccount=istio-system:istio-reader-service-account \
    --dry-run -o yaml  | kc apply -f -
}

generate_istio_control_plane() {
  local CONTEXT=${1}
  ic() { istioctl --context ${CONTEXT} $@; }

  ic x multicluster generate -f ${MESH_TOPOLOGY_FILENAME} --from ${BASE_FILENAME} > ${WORKDIR}/istio-${CONTEXT}.yaml
}

apply_istio_control_plane() {
  local CONTEXT=${1}
  ic() { istioctl --context ${CONTEXT} $@; }

  # TODO remove the hub/tag or make it optional and parameterize it.
  ic manifest apply -f ${WORKDIR}/istio-${CONTEXT}.yaml --set hub=docker/istio.io --set tag=1.4.0-beta.3
}


first_phase() {
  echo "first phase update"
  for CONTEXT in $(kubectl config get-contexts -o name); do
    create_and_install_intermediate_ca ${CONTEXT}

    apply_missing_service_account ${CONTEXT}
    generate_istio_control_plane ${CONTEXT}
    apply_istio_control_plane ${CONTEXT}
  done
}

# clusters are not fully joined at this point

# TODO(ayj) - remove when the istioctl supports the --wait-for-gateway option
wait_for_gateways() {
  echo "waiting for gateway external IPs to be allocated"
  for CONTEXT in $(kubectl config get-contexts -o name); do
    local CONTEXT=${1}
    kc() { kubectl --context ${CONTEXT} $@; }
    (
      while true; do
        IP=(kc istio-system get svc istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
        if [[ $? -eq 0 ]]; then
          echo "Ingress gateway external IP for cluster ${CONTEXT} is ready: ${IP}"
         return 0
       fi
       sleep 1
      done
    )
    wait
  done
}

reapply_config_with_networks() {
  echo "updating the cluster's mesh network configurationwith allocated gateway IPs"
  for CONTEXT in $(kubectl config get-contexts -o name); do
    generate_istio_control_plane ${CONTEXT}
    apply_istio_control_plane ${CONTEXT}
  done
}

create_passthrough_gateway() {
  for CONTEXT in $(kubectl config get-contexts -o name); do
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
done
}

install_bookinfo() {
  for CONTEXT in $(kubectl config get-contexts -o name); do
    kc() { kubectl --context ${CONTEXT} $@; }

    kc label namespace default istio-injection=enabled --overwrite
    kc apply -f ../bookinfo/platform/kube/bookinfo.yaml
    kc apply -f ../bookinfo/networking/bookinfo-gateway.yaml
  done
}

first_phase
wait_for_gateways
reapply_config_with_networks
create_passthrough_gateway

# join everything togeather
istioctl x multicluster apply -f ${MESH_TOPOLOGY_FILENAME}

install_bookinfo

istioctl x mc describe -f ${MESH_TOPOLOGY_FILENAME}
