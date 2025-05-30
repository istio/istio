#!/bin/bash
#
# Copyright Istio Authors. All Rights Reserved.
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
#
################################################################################
#
# Script to configure and start the Istio sidecar.

set -ea

# Load optional config variables
ISTIO_SIDECAR_CONFIG=${ISTIO_SIDECAR_CONFIG:-./var/lib/istio/envoy/sidecar.env}
if [[ -r ${ISTIO_SIDECAR_CONFIG} ]]; then
  # shellcheck disable=SC1090
  . "$ISTIO_SIDECAR_CONFIG"
fi

# Load config variables ISTIO_SYSTEM_NAMESPACE, CONTROL_PLANE_AUTH_POLICY
ISTIO_CLUSTER_CONFIG=${ISTIO_CLUSTER_CONFIG:-./var/lib/istio/envoy/cluster.env}
if [[ -r ${ISTIO_CLUSTER_CONFIG} ]]; then
  # shellcheck disable=SC1090
  . "$ISTIO_CLUSTER_CONFIG"
fi
set +a

# Set defaults
ISTIO_BIN_BASE=${ISTIO_BIN_BASE:-/usr/local/bin}
ISTIO_LOG_DIR=${ISTIO_LOG_DIR:-./var/log/istio}
NS=${ISTIO_NAMESPACE:-default}
SVC=${ISTIO_SERVICE:-rawvm}
ISTIO_SYSTEM_NAMESPACE=${ISTIO_SYSTEM_NAMESPACE:-istio-system}

# If set, override the default
CONTROL_PLANE_AUTH_POLICY=${ISTIO_CP_AUTH:-"MUTUAL_TLS"}

if [ -z "${ISTIO_SVC_IP:-}" ]; then
  ISTIO_SVC_IP=$(hostname --all-ip-addresses | cut -d ' ' -f 1)
fi

if [ -z "${POD_NAME:-}" ]; then
  POD_NAME=$(hostname -s)
fi


# set ISTIO_CUSTOM_IP_TABLES to true if you would like to ignore this step
if [ "${ISTIO_CUSTOM_IP_TABLES}" != "true" ] ; then
    if [[ ${1-} == "clean" ]] ; then
      # clean the previous Istio iptables chains.
      "${ISTIO_BIN_BASE}/pilot-agent" istio-iptables --cleanup-only --reconcile
      exit 0
    fi

    # Init option will only initialize iptables.
    if [[ ${1-} == "init" || ${1-} == "-p" ]] ; then
      # clean the previous Istio iptables chains. This part is different from the init image mode,
      # where the init container runs in a fresh environment and there cannot be previous Istio chains
      "${ISTIO_BIN_BASE}/pilot-agent" istio-iptables "${@}" --cleanup-only --reconcile

      # Update iptables, based on current config. This is for backward compatibility with the init image mode.
      # The sidecar image can replace the k8s init image, to avoid downloading 2 different images.
      "${ISTIO_BIN_BASE}/pilot-agent" istio-iptables "${@}"
      exit 0
    fi

    if [[ ${1-} != "run" ]] ; then
      # clean the previous Istio iptables chains. This part is different from the init image mode,
      # where the init container runs in a fresh environment and there cannot be previous Istio chains
      "${ISTIO_BIN_BASE}/pilot-agent" istio-iptables --cleanup-only --reconcile

      # Update iptables, based on config file
      "${ISTIO_BIN_BASE}/pilot-agent" istio-iptables
    fi
fi

EXEC_USER=${EXEC_USER:-istio-proxy}
if [ "${ISTIO_INBOUND_INTERCEPTION_MODE}" = "TPROXY" ] ; then
  # In order to allow redirect inbound traffic using TPROXY, run envoy with the CAP_NET_ADMIN capability.
  # This allows configuring listeners with the "transparent" socket option set to true.
  EXEC_USER=root
fi

# The default matches the default istio.yaml - use sidecar.env to override ISTIO_PILOT_PORT or CA_ADDR if you
# enable auth. This requires node-agent to be running.
DEFAULT_PILOT_ADDRESS="istiod.${ISTIO_SYSTEM_NAMESPACE}.svc:15012"
CUSTOM_PILOT_ADDRESS="${PILOT_ADDRESS:-}"
if [ -z "${CUSTOM_PILOT_ADDRESS}" ] && [ -n "${ISTIO_PILOT_PORT:-}" ]; then
  CUSTOM_PILOT_ADDRESS=istiod.${ISTIO_SYSTEM_NAMESPACE}.svc:${ISTIO_PILOT_PORT}
fi

# CA_ADDR > PILOT_ADDRESS > ISTIO_PILOT_PORT
CA_ADDR=${CA_ADDR:-${CUSTOM_PILOT_ADDRESS:-${DEFAULT_PILOT_ADDRESS}}}
PROV_CERT=${PROV_CERT-./etc/certs}
OUTPUT_CERTS=${OUTPUT_CERTS-./etc/certs}

export PROV_CERT
export OUTPUT_CERTS
export CA_ADDR

# If predefined ISTIO_AGENT_FLAGS is null, make it an empty string.
ISTIO_AGENT_FLAGS=${ISTIO_AGENT_FLAGS:-}
# Split ISTIO_AGENT_FLAGS by spaces.
IFS=' ' read -r -a ISTIO_AGENT_FLAGS_ARRAY <<< "$ISTIO_AGENT_FLAGS"

DEFAULT_PROXY_CONFIG="
serviceCluster: $SVC
controlPlaneAuthPolicy: ${CONTROL_PLANE_AUTH_POLICY}
"
if [ -n "${CUSTOM_PILOT_ADDRESS}" ]; then
  PROXY_CONFIG="$PROXY_CONFIG
discoveryAddress: ${CUSTOM_PILOT_ADDRESS}
"
fi

# PROXY_CONFIG > PILOT_ADDRESS > ISTIO_PILOT_PORT
export PROXY_CONFIG=${PROXY_CONFIG:-${DEFAULT_PROXY_CONFIG}}

if [ "${EXEC_USER}" == "${USER:-}" ] ; then
  # if started as istio-proxy (or current user), do a normal start, without
  # redirecting stderr.
  INSTANCE_IP=${ISTIO_SVC_IP} POD_NAME=${POD_NAME} POD_NAMESPACE=${NS} "${ISTIO_BIN_BASE}/pilot-agent" proxy "${ISTIO_AGENT_FLAGS_ARRAY[@]}"
else
  # Will run: ${ISTIO_BIN_BASE}/envoy -c $ENVOY_CFG --restart-epoch 0 --drain-time-s 2 --parent-shutdown-time-s 3 --service-cluster $SVC --service-node 'sidecar~${ISTIO_SVC_IP}~${POD_NAME}.${NS}.svc.cluster.local~${NS}.svc.cluster.local' $ISTIO_DEBUG >${ISTIO_LOG_DIR}/istio.log" istio-proxy
  exec sudo -E -u "${EXEC_USER}" -s /bin/bash -c "INSTANCE_IP=${ISTIO_SVC_IP} POD_NAME=${POD_NAME} POD_NAMESPACE=${NS} exec ${ISTIO_BIN_BASE}/pilot-agent proxy ${ISTIO_AGENT_FLAGS_ARRAY[*]} 2>> ${ISTIO_LOG_DIR}/istio.err.log >> ${ISTIO_LOG_DIR}/istio.log"
fi
