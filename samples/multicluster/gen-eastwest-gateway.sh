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

if [[ -z "${CLUSTER:-}" ]]; then
  echo The CLUSTER environment variable must be set.
  exit 1
fi

if [[ -z "${NETWORK:-}" ]]; then
  echo The NETWORK environment variable must be set.
  exit 1
fi

# Generate the YAML for the east-west gateway.
istioctl manifest generate "${@}" -f - <<EOF
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  # Only generate a gateway component defined below.
  # Using this with "istioctl install" will reconcile and remove existing control-plane components.
  # Instead use "istioctl manifest generate" or "kubectl create" if using the istio operator.
  profile: empty
  values:
    global:
      network: ${NETWORK}
      multiCluster:
        clusterName: ${CLUSTER}
  components:
    ingressGateways:
      - name: istio-eastwestgateway
        label:
          istio: eastwestgateway
          app: istio-eastwestgateway
        enabled: true
        k8s:
          env:
            # traffic through this gateway should be routed inside the network
            - name: ISTIO_META_REQUESTED_NETWORK_VIEW
              value: ${NETWORK}
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
