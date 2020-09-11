#!/bin/bash

# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

if [[ -n "${CLUSTER:-}" ]]; then
  echo The CLUSTER environment variable must be set.
  exit 1
fi

if [[ -n "${NETWORK:-}" ]]; then
  echo The NETWORK environment variable must be set.
  exit 1
fi

if [[ -n "${MESH_ID:-}" ]]; then
  echo The NETWORK environment variable must be set.
  exit 1
fi

cat << EOF
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: empty
  values:
    global:
      network: ${NETWORK}
      multiCluster:
        clusterName: ${CLUSTER}
  components:
    ingressGateways:
      - name: istio-east-west-gateway
        label:
          istio: east-west-gateway
          app: istio-east-west-gateway
        enabled: true
        k8s:
          env:
            # traffic through this gateway should be routed inside the network
            - name: ISTIO_META_REQUESTED_NETWORK_VIEW
              value: ${NETWORK}
EOF