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
  echo "    $0 --cluster-name cluster1 --k8s-release 1.22.1 --ip-space 255"
  echo ""
  echo "Where:"
  echo "    -n|--cluster-name  - name of the k8s cluster to be created"
  echo "    -r|--k8s-release   - the release of the k8s to setup, latest available if not given"
  echo "    -s|--ip-space      - the 2nd to the last part for public ip addresses, 255 if not given, valid range: 0-255."
  echo "    -m|--mode          - setup the required number of nodes per deployment model. Values are sidecar (1 node) or ambient (minimum of 2)"
  echo "    -w|--worker-nodes  - the number of worker nodes to create. Default is 1"
  echo "    --pod-subnet       - the pod subnet to specify. Default is 10.244.0.0/16 for IPv4 and fd00:10:244::/56 for IPv6"
  echo "    --service-subnet   - the service subnet to specify. Default is 10.96.0.0/16 for IPv4 and fd00:10:96::/112 for IPv6"
  echo "    -i|--ip-family     - ip family to be supported, default is ipv4 only. Value should be ipv4, ipv6, or dual"
  echo "    --ipv6gw           - set ipv6 as the gateway, necessary for dual-stack IPv6-preferred clusters"
  echo "    -h|--help          - print the usage of this script"
}

# Setup default values
CLUSTERNAME="cluster1"
K8SRELEASE=""
IPSPACE=255
IPFAMILY="ipv4"
MODE="sidecar"
NUMNODES=""
PODSUBNET=""
SERVICESUBNET=""
IPV6GW=false
API_SERVER_ADDRESS=""

# Handling parameters
while [[ $# -gt 0 ]]; do
  optkey="$1"
  case $optkey in
    -n|--cluster-name)
      CLUSTERNAME="$2"; shift 2;;
    -r|--k8s-release)
      K8SRELEASE="--image=kindest/node:v$2"; shift 2;;
    -s|--ip-space)
      IPSPACE="$2"; shift 2;;
    -m|--mode)
      MODE="$2"; shift 2;;
    -w|--worker-nodes)
      NUMNODES="$2"; shift 2;;
    --pod-subnet)
      PODSUBNET="$2"; shift 2;;
    --service-subnet)
      SERVICESUBNET="$2"; shift 2;;
    -i|--ip-family)
      IPFAMILY="${2,,}";shift 2;;
    --ipv6gw)
      IPV6GW=true; shift;;
    -h|--help)
      printHelp; exit 0;;
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

if [[ "${MODE}" == "ambient" ]]; then
  NUMNODES=${NUMNODES:-2}
fi

NODES=$(cat <<-EOM
nodes:
- role: control-plane
EOM
)

if [[ -n "${NUMNODES}" ]]; then
for _ in $(seq 1 "${NUMNODES}"); do
  NODES+=$(printf "\n%s" "- role: worker")
done
fi

OS=$(uname -s)
case $OS in
    Darwin)
        if [ -z "${API_SERVER_ADDRESS}" ]; then
            # If you are using Docker on Windows or Mac,
            # you will need to use an IPv4 port forward for the API Server
            # from the host because IPv6 port forwards don’t work on these platforms.
            API_SERVER_ADDRESS="127.0.0.1"
            echo "Using API server address: ${API_SERVER_ADDRESS} Darwin"
        fi
        ;;
esac

CONFIG=$(cat <<-EOM
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
${FEATURES}
name: ${CLUSTERNAME}
${NODES}
networking:
  apiServerAddress: ${API_SERVER_ADDRESS}
  ipFamily: ${IPFAMILY}
EOM
)

if [[ -n "${PODSUBNET}" ]]; then
  CONFIG+=$(printf "\n%s" "  podSubnet: \"${PODSUBNET}\"")
fi

if [[ -n "${SERVICESUBNET}" ]]; then
  CONFIG+=$(printf "\n%s" "  serviceSubnet: \"${SERVICESUBNET}\"")
fi

# Create k8s cluster using the giving release and name
if [[ -z "${K8SRELEASE}" ]]; then
  cat << EOF | kind create cluster --config -
${CONFIG}
EOF
else
  cat << EOF | kind create cluster "${K8SRELEASE}" --config -
${CONFIG}
EOF
fi

# Setup cluster context
kubectl cluster-info --context "kind-${CLUSTERNAME}"

# Setup metallb using v0.14.9
kubectl apply -f https://raw.githubusercontent.com/metallb/metallb/v0.14.9/config/manifests/metallb-native.yaml

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
  ipv4Range="- ${ipv4Prefix}.${IPSPACE}.200-${ipv4Prefix}.${IPSPACE}.240"
  ipv6Range=""
elif [[ "${IPFAMILY}" == "ipv6" ]]; then
  addrName="GlobalIPv6Address"
  ipv4Range=""
  ipv6Range="- ${ipv6Prefix}::${IPSPACE}:200-${ipv6Prefix}::${IPSPACE}:240"
else
  if [[ "${IPV6GW}" == "true" ]]; then
    addrName="GlobalIPv6Address"
  fi

  ipv4Range="- ${ipv4Prefix}.${IPSPACE}.200-${ipv4Prefix}.${IPSPACE}.240"
  ipv6Range="- ${ipv6Prefix}::${IPSPACE}:200-${ipv6Prefix}::${IPSPACE}:240"
fi

# utility function to wait for pods to be ready
function waitForPods() {
  ns=$1
  lb=$2
  waittime=$3
  # Wait for the pods to be ready in the given namespace with label
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
    elif [[ "${IPFAMILY}" == "dual" ]] && [[ "${IPV6GW}" == "true" ]]; then
      ip="[${ip}]"
    fi
    kubectl config set clusters.kind-"${CLUSTERNAME}".server https://"${ip}":6443
    break
  fi
  echo 'Waiting for public IP address to be available...'
  sleep 3
done

echo "Kubernetes cluster ${CLUSTERNAME} was created successfully!"
