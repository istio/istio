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

ARGS=("${@:-}")

set -euo pipefail

# single-cluster installations may need this gateway to allow VMs to get discovery
IOP=$(cat <<EOF
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  # Only generate a gateway component defined below.
  # Using this with "istioctl install" will reconcile and remove existing control-plane components.
  # Instead we use "istioctl manifest generate" and "kubectl apply".
  profile: empty
  components:
    ingressGateways:
      - name: istio-eastwestgateway
        label:
          istio: eastwestgateway
          app: istio-eastwestgateway
        enabled: true
        k8s:
          service:
            ports:
              - name: status-port
                port: 15021
                targetPort: 15021
              - name: mtls
                port: 15443
                targetPort: 15443
              - name: tcp-istiod
                port: 15012
                targetPort: 15012
              - name: tcp-webhook
                port: 15017
                targetPort: 15017
              - name: tcp-dns-tls
                port: 853
                targetPort: 8853
EOF
)

SINGLE_CLUSTER="${SINGLE_CLUSTER:-0}"
if [[ "${SINGLE_CLUSTER}" -eq 0 ]]; then
  if [[ -z "${CLUSTER:-}" ]]; then
  echo The CLUSTER environment variable must be set.
  exit 1
  fi
  if [[ -z "${NETWORK:-}" ]]; then
    echo The NETWORK environment variable must be set.
    exit 1
  fi
  if [[ -z "${MESH:-}" ]]; then
    echo The MESH environment variable must be set.
    exit 1
  fi
  IOP=$(cat <<EOF
$IOP
          env:
            # traffic through this gateway should be routed inside the network
            - name: ISTIO_META_REQUESTED_NETWORK_VIEW
              value: ${NETWORK}
  values:
    global:
      meshID: ${MESH}
      network: ${NETWORK}
      multiCluster:
        clusterName: ${CLUSTER}
EOF
)
fi

if [[ "${#}" -gt 0 ]]; then
  GEN_PARAMS=("${ARGS[@]}")
fi
GEN_PARAMS+=("-f" "-")

# Generate the YAML for the east-west gateway.
istioctl manifest generate "${GEN_PARAMS[@]}" <<EOF
$IOP
EOF
