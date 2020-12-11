#!/bin/bash
#
# Copyright Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

set -euo pipefail

SINGLE_CLUSTER=0
REVISION=""
while (( "$#" )); do
  case "$1" in
    # Node images can be found at https://github.com/kubernetes-sigs/kind/releases
    # For example, kindest/node:v1.14.0
    --single-cluster)
      SINGLE_CLUSTER=1
      shift
    ;;
    --cluster)
      CLUSTER=$2
      shift 2
    ;;
    --network)
      NETWORK=$2
      shift 2
    ;;
    --mesh)
      MESH=$2
      shift 2
    ;;
    --revision)
      REVISION=$2
      shift 2
    ;;
    -*)
      echo "Error: Unsupported flag $1" >&2
      exit 1
      ;;
  esac
done


# single-cluster installations may need this gateway to allow VMs to get discovery
# for non-single cluster, we add additional topology information
SINGLE_CLUSTER="${SINGLE_CLUSTER:-0}"
if [[ "${SINGLE_CLUSTER}" -eq 0 ]]; then
  if [[ -z "${CLUSTER:-}" ]] || [[ -z "${NETWORK:-}" ]] || [[ -z "${MESH:-}" ]]; then
    echo "Must specify either --single-cluster or --mesh, --cluster, and --network."
    exit 1
  fi
fi

# base
IOP=$(cat <<EOF
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: eastwest
spec:
  revision: "${REVISION}"
  profile: empty
  components:
    ingressGateways:
      - name: istio-eastwestgateway
        label:
          istio: eastwestgateway
          app: istio-eastwestgateway
EOF
)

# mark this as a multi-network gateway
if [[ "${SINGLE_CLUSTER}" -eq 0 ]]; then
  IOP=$(cat <<EOF
$IOP
          topology.istio.io/network: $NETWORK
EOF
)
fi

# env
IOP=$(cat <<EOF
$IOP
        enabled: true
        k8s:
          env:
            # sni-dnat adds the clusters required for AUTO_PASSTHROUGH mode
            - name: ISTIO_META_ROUTER_MODE
              value: "sni-dnat"
EOF
)
if [[ "${SINGLE_CLUSTER}" -eq 0 ]]; then
  IOP=$(cat <<EOF
$IOP
            # traffic through this gateway should be routed inside the network
            - name: ISTIO_META_REQUESTED_NETWORK_VIEW
              value: ${NETWORK}
EOF
)
fi

# Ports
IOP=$(cat <<EOF
$IOP
          service:
            ports:
              - name: status-port
                port: 15021
                targetPort: 15021
              - name: tls
                port: 15443
                targetPort: 15443
              - name: tls-istiod
                port: 15012
                targetPort: 15012
              - name: tls-webhook
                port: 15017
                targetPort: 15017
EOF
)

# additional multicluster/multinetwork meta
if [[ "${SINGLE_CLUSTER}" -eq 0 ]]; then
  IOP=$(cat <<EOF
$IOP
  values:
    global:
      meshID: ${MESH}
      network: ${NETWORK}
      multiCluster:
        clusterName: ${CLUSTER}
EOF
)
fi

echo "$IOP"
