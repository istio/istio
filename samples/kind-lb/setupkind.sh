#!/bin/bash
# Copyright Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#
set -e

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
  echo "    -h|--help          - print the usage of this script"
}

# Setup default values
CLUSTERNAME="cluster1"
K8SRELEASE=""
IPSPACE=255

# Handling parameters
while [[ $# -gt 0 ]]; do
  optkey="$1"
  case $optkey in
    -h|--help)
      printHelp; exit 0;;
    -n|--cluster-name)
      CLUSTERNAME="$2";shift;shift;;
    -r|--k8s-release)
      K8SRELEASE="--image=kindest/node:v$2";shift;shift;;
    -s|--ip-space)
      IPSPACE="$2";shift;shift;;
    *) # unknown option
      echo "parameter $1 is not supported"; exit 1;;
  esac
done

# Create k8s cluster using the giving release and name
if [[ -z "${K8SRELEASE}" ]]; then
  kind create cluster --name "${CLUSTERNAME}"
else
  kind create cluster "${K8SRELEASE}" --name "${CLUSTERNAME}"
fi
# Setup cluster context
kubectl cluster-info --context "kind-${CLUSTERNAME}"

# Setup metallb using v0.12.
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.12/manifests/namespace.yaml
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.12/manifests/metallb.yaml

# The following makes sure that the kube configuration for the cluster is not
# using the loopback ip as part of the api server endpoint. Without this,
# multiple clusters would not be able to interact with each other.
PREFIX=$(docker network inspect -f '{{range .IPAM.Config }}{{ .Gateway }}{{end}}' kind | cut -d '.' -f1,2)

# Now configure the loadbalancer public IP range
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  namespace: metallb-system
  name: config
data:
  config: |
    address-pools:
    - name: default
      protocol: layer2
      addresses:
      - $PREFIX.$IPSPACE.200-$PREFIX.$IPSPACE.240
EOF

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
