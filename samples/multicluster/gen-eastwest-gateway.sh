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

ARGS="${@:-}"

set -euo pipefail

ROOT="$( cd "$(dirname "$0")" >/dev/null 2>&1 ; pwd -P )"

SINGLE_CLUSTER="${SINGLE_CLUSTER:-0}"

IOP=$(cat "${ROOT}/eastwest-gateway.yaml")

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


# Generate the YAML for the east-west gateway.
# shellcheck disable=SC2068
istioctl manifest generate ${ARGS} -f - <<EOF
$IOP
EOF
