#!/bin/bash
# Copyright Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#
set -e

# This script can only produce desired results on Linux systems.
ENVOS=$(uname 2>/dev/null || true)
if [[ "${ENVOS}" != "Linux" ]]; then
  echo "Your system is not supported by this script. Only Linux is supported"
  exit 1
fi

# Check prerequisites
REQUISITES=("kubectl" "kind" "docker")
for item in "${REQUISITES[@]}"; do
  if [[ -z $(which "${item}") ]]; then
    echo "${item} cannot be found on your system, please install ${item}"
    exit 1
  fi
done

# Function to print the usage message
function printHelp() {
  echo "Usage: "
  echo "    $0 --cluster-name cluster1 --k8s-release 1.22.1 --ip-octet 255"
  echo ""
  echo "Where:"
  echo "    -n|--cluster-name  - name of the k8s cluster to be created"
  echo "    -r|--k8s-release   - the release of the k8s to setup, latest available if not given"
  echo "    -s|--ip-octet      - the 3rd octet for public ip addresses, 255 if not given, valid range: 0-255"
  echo "    -i|--ip-family     - the ipFamily type for the kind cluster. Supported values: ipv4 (default), ipv6, dual"
  echo "    -h|--help          - print the usage of this script"
}

# Setup default values
CLUSTERNAME="cluster1"
K8SRELEASE=""
IPSPACE=255
IPFAMILY="ipv4"

# Handling parameters
while [[ $# -gt 0 ]]; do
  optkey="$1"
  case $optkey in
    -h|--help)
      printHelp; exit 0;;
    -n|--cluster-name)
      CLUSTERNAME="$2"; shift 2;;
    -r|--k8s-release)
      K8SRELEASE="--image=kindest/node:v$2"; shift 2;;
    -s|--ip-space)
      IPSPACE="$2"; shift 2;;
    -i|--ip-family)
      IPFAMILY="$2"; shift 2;;
    *) # unknown option
      echo "parameter $1 is not supported"; printHelp; exit 1;;
  esac
done

tmpfile=$(mktemp)
cat <<EOF >> "${tmpfile}"
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  ipFamily: ${IPFAMILY}
EOF


# Create k8s cluster using the giving release and name
if [[ -z "${K8SRELEASE}" ]]; then
  kind create cluster --name "${CLUSTERNAME}" --config="${tmpfile}" --wait 5m
else
  kind create cluster "${K8SRELEASE}" --name "${CLUSTERNAME}" --config="${tmpfile}" --wait 5m
fi

rm "${tmpfile}"

# Setup cluster context
# kubectl cluster-info --context "kind-${CLUSTERNAME}"

# Setup metallb using v0.13.6.
if [[ "${IPFAMILY}" == "ipv4" ]];
then
  kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.13.6/config/manifests/metallb-native.yaml
else
  kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.13.6/config/manifests/metallb-frr.yaml
fi
kubectl wait pods --namespace metallb-system --for=condition=Ready --timeout=5m --all

# The following makes sure that the kube configuration for the cluster is not
# using the loopback ip as part of the api server endpoint. Without this,
# multiple clusters would not be able to interact with each other.
PREFIX=$(docker network inspect -f '{{range .IPAM.Config }}{{ .Gateway }}{{end}}' kind | cut -d '.' -f1,2)

addresses="  - ${PREFIX}.${IPSPACE}.200-${PREFIX}.${IPSPACE}.240"

# Now configure the loadbalancer public IP range
if [[ "${IPFAMILY}" == "ipv6" ]]; then
  SUBNETS=$(docker network inspect -f '{{range .IPAM.Config }}{{.Subnet}} {{end}}' kind)
  for subnet in ${SUBNETS};
  do
    addresses=""
    if [[ "${subnet}" == *":"* ]]; then
      addresses="  - ${subnet}"
    fi 
  done
elif [[ "${IPFAMILY}" == "dual" ]]; then
  SUBNETS=$(docker network inspect -f '{{range .IPAM.Config }}{{.Subnet}} {{end}}' kind)
  addresses=""
  for subnet in ${SUBNETS};
  do
    addresses="  - ${subnet}\n${addresses}"
  done
fi


tmpfile=$(mktemp)
printf \
"apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: default
  namespace: metallb-system
spec:
  addresses:
%b" "${addresses}" >> "${tmpfile}"

kubectl apply -f "${tmpfile}"
rm "${tmpfile}"

# Wait for the public IP address to become available.
while : ; do
  IP=$(docker inspect -f '{{range.NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${CLUSTERNAME}"-control-plane)
  if [[ -n "${IP}" ]]; then
    # Change the kubeconfig file not to use the loopback IP
    kubectl config set clusters.kind-"${CLUSTERNAME}".server https://"${IP}":6443
    break
  fi
  echo 'Waiting for public IP address to be available...'
  sleep 3
done

echo "Kubernetes cluster ${CLUSTERNAME} was created successfully!"
