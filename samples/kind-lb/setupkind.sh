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
  echo "    $0 --cluster-name cluster1 --k8s-release 1.22.1 --ip-space 255"
  echo ""
  echo "Where:"
  echo "    -n|--cluster-name  - name of the k8s cluster to be created"
  echo "    -r|--k8s-release   - the release of the k8s to setup, latest available if not given"
  echo "    -s|--ip-space      - the 2rd to the last part for public ip addresses, 255 if not given, valid range: 0-255"
  echo "    -i|--ip-family     - ip family to be supported, default is ipv4 only. Value should be ipv4, ipv6, or dual"
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
      IPFAMILY="${2,,}";shift 2;;
    *) # unknown option
      echo "parameter $1 is not supported"; printHelp; exit 1;;
  esac
done

# This block is to setup kind to have a local image repo to push
# images using localhost:5000, to use this feature, start up
# a registry container such as gcr.io/istio-testing/registry, then
# connect it to the docker network where kind nodes are running on
# which normally will be called kind
FEATURES=$(cat << EOF
featureGates:
  MixedProtocolLBService: true
  GRPCContainerProbe: true
kubeadmConfigPatches:
  - |
    apiVersion: kubeadm.k8s.io/v1beta2
    kind: ClusterConfiguration
    metadata:
      name: config
    etcd:
      local:
        # Run etcd in a tmpfs (in RAM) for performance improvements
        dataDir: /tmp/kind-cluster-etcd
    # We run single node, drop leader election to reduce overhead
    controllerManagerExtraArgs:
      leader-elect: "false"
    schedulerExtraArgs:
      leader-elect: "false"
    apiServer:
      extraArgs:
        "service-account-issuer": "kubernetes.default.svc"
        "service-account-signing-key-file": "/etc/kubernetes/pki/sa.key"
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry.mirrors."localhost:5000"]
      endpoint = ["http://kind-registry:5000"]
EOF
)

validIPFamilies=("ipv4" "ipv6" "dual")
# Validate if the ip family value is correct.
isValid="false"
for family in "${validIPFamilies[@]}"; do
  if [[ "$family" == "${IPFAMILY}" ]]; then
    isValid="true"
    break
  fi
done

if [[ "${isValid}" == "false" ]]; then
  echo "${IPFAMILY} is not valid ip family, valid values are ipv4, ipv6 or dual"
  exit 1
fi

# Create k8s cluster using the giving release and name
if [[ -z "${K8SRELEASE}" ]]; then
  cat << EOF | kind create cluster --config -
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
${FEATURES}
name: ${CLUSTERNAME}
networking:
  ipFamily: ${IPFAMILY}
EOF
else
  cat << EOF | kind create cluster "${K8SRELEASE}" --config -
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
${FEATURES}
name: ${CLUSTERNAME}
networking:
  ipFamily: ${IPFAMILY}
EOF
fi

# Setup cluster context
kubectl cluster-info --context "kind-${CLUSTERNAME}"

# Setup metallb using v0.13.6
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.13.6/config/manifests/metallb-native.yaml

addrName="IPAddress"
ipv4Prefix=""
ipv6Prefix=""

# Get both ipv4 and ipv6 gateway for the cluster
gatewaystr=$(docker network inspect -f '{{range .IPAM.Config }}{{ .Gateway }} {{end}}' kind | cut -f1,2)
read -r -a gateways <<< "${gatewaystr}"
for gateway in "${gateways[@]}"; do
  if [[ "$gateway" == *"."* ]]; then
    ipv4Prefix=$(echo "${gateway}" |cut -d'.' -f1,2)
  else
    ipv6Prefix=$(echo "${gateway}" |cut -d':' -f1,2,3,4)
  fi
done

if [[ "${IPFAMILY}" == "ipv4" ]]; then
  addrName="IPAddress"
  ipv4Range="- ${ipv4Prefix}.$IPSPACE.200-${ipv4Prefix}.$IPSPACE.240"
  ipv6Range=""
elif [[ "${IPFAMILY}" == "ipv6" ]]; then
  ipv4Range=""
  ipv6Range="- ${ipv6Prefix}::$IPSPACE:200-${ipv6Prefix}::$IPSPACE:240"
  addrName="GlobalIPv6Address"
else
  ipv4Range="- ${ipv4Prefix}.$IPSPACE.200-${ipv4Prefix}.$IPSPACE.240"
  ipv6Range="- ${ipv6Prefix}::$IPSPACE:200-${ipv6Prefix}::$IPSPACE:240"
fi

# utility function to wait for pods to be ready
function waitForPods() {
  ns=$1
  lb=$2
  waittime=$3
  # Wait for the pods to be ready in the given namespace with lable
  while : ; do
    res=$(kubectl wait --context "kind-${CLUSTERNAME}" -n "${ns}" pod \
      -l "${lb}" --for=condition=Ready --timeout="${waittime}s" 2>/dev/null ||true)
    if [[ "${res}" == *"condition met"* ]]; then
      break
    fi
    echo "Waiting for pods in namespace ${ns} with label ${lb} to be ready..."
    sleep "${waittime}"
  done
}

waitForPods metallb-system app=metallb 10

# Now configure the loadbalancer public IP range
cat <<EOF | kubectl apply -f -
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  namespace: metallb-system
  name: address-pool
spec:
  addresses:
    ${ipv4Range}
    ${ipv6Range}
---
apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: empty
  namespace: metallb-system
EOF

# Wait for the public IP address to become available.
while : ; do
  ip=$(docker inspect -f '{{range.NetworkSettings.Networks}}{{.'${addrName}'}}{{end}}' "${CLUSTERNAME}"-control-plane)
  if [[ -n "${ip}" ]]; then
    #Change the kubeconfig file not to use the loopback IP
    if [[ "${IPFAMILY}" == "ipv6" ]]; then
      ip="[${ip}]"
    fi
    kubectl config set clusters.kind-"${CLUSTERNAME}".server https://"${ip}":6443
    break
  fi
  echo 'Waiting for public IP address to be available...'
  sleep 3
done

echo "Kubernetes cluster ${CLUSTERNAME} was created successfully!"
