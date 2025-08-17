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
AMBIENT=0
while (( "$#" )); do
  case "$1" in
    --single-cluster)
      SINGLE_CLUSTER=1
      shift
    ;;
    --cluster)
      # No longer does anything, but keep it around to avoid breaking users
      shift 2
    ;;
    --network)
      NETWORK=$2
      shift 2
    ;;
    --mesh)
      # No longer does anything, but keep it around to avoid breaking users
      shift 2
    ;;
    --revision)
      REVISION=$2
      shift 2
    ;;
    --ambient)
      AMBIENT=1
      shift
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
  if [[ -z "${NETWORK:-}" ]]; then
    echo "Must specify either --single-cluster or --network."
    exit 1
  fi
fi

# when ambient is enabled, we don't need to create an istioOperator resource
# but we still need to create the gateway deployment
if [[ "${AMBIENT}" -eq 1 ]]; then
  if [[ -z "${NETWORK}" ]]; then
    echo "Must specify --network with --ambient."
    exit 1
  fi
GW=$(cat <<EOF
apiVersion: gateway.networking.k8s.io/v1beta1
kind: Gateway
metadata:
  name: istio-eastwestgateway
  namespace: istio-system
  labels:
    topology.istio.io/network: "${NETWORK}"
spec:
  gatewayClassName: "istio-east-west"
  listeners:
    - name: mesh
      port: 15008
      protocol: HBONE
      tls:
        mode: Terminate
        options:
          gateway.istio.io/tls-terminate-mode: ISTIO_MUTUAL
EOF
)
  echo "$GW"
  exit 0
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
EOF
)
if [[ "${SINGLE_CLUSTER}" -eq 0 ]]; then
  IOP=$(cat <<EOF
$IOP
          env:
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

# Gateway injection template
IOP=$(cat <<EOF
$IOP
  values:
    gateways:
      istio-ingressgateway:
        injectionTemplate: gateway
EOF
)

# additional multicluster/multinetwork meta
if [[ "${SINGLE_CLUSTER}" -eq 0 ]]; then
  IOP=$(cat <<EOF
$IOP
    global:
      network: ${NETWORK}
EOF
)
fi

echo "$IOP"
