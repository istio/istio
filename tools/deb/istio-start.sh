#!/bin/bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
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

set -e

# Load optional config variables
ISTIO_SIDECAR_CONFIG=${ISTIO_SIDECAR_CONFIG:-/var/lib/istio/envoy/sidecar.env}
if [[ -r ${ISTIO_SIDECAR_CONFIG} ]]; then
  . $ISTIO_SIDECAR_CONFIG
fi

# Load config variables ISTIO_SYSTEM_NAMESPACE, CONTROL_PLANE_AUTH_POLICY
ISTIO_CLUSTER_CONFIG=${ISTIO_CLUSTER_CONFIG:-/var/lib/istio/envoy/cluster.env}
if [[ -r ${ISTIO_CLUSTER_CONFIG} ]]; then
  . $ISTIO_CLUSTER_CONFIG
fi

# Set defaults
ISTIO_BIN_BASE=${ISTIO_BIN_BASE:-/usr/local/bin}
ISTIO_LOG_DIR=${ISTIO_LOG_DIR:-/var/log/istio}
NS=${ISTIO_NAMESPACE:-default}
SVC=${ISTIO_SERVICE:-rawvm}
ISTIO_SYSTEM_NAMESPACE=${ISTIO_SYSTEM_NAMESPACE:-istio-system}

# The default matches the default istio.yaml - use sidecar.env to override this if you
# enable auth. This requires node-agent to be running.
ISTIO_PILOT_PORT=${ISTIO_PILOT_PORT:-8080}
ISTIO_CP_AUTH=${ISTIO_CP_AUTH:-NONE}


if [ -z "${ISTIO_SVC_IP:-}" ]; then
  ISTIO_SVC_IP=$(hostname --ip-address)
fi

if [ -z "${POD_NAME:-}" ]; then
  POD_NAME=$(hostname -s)
fi

# Init option will only initialize iptables. Can be used
if [[ ${1-} == "init" || ${1-} == "-p" ]] ; then
  # Update iptables, based on current config. This is for backward compatibility with the init image mode.
  # The sidecar image can replace the k8s init image, to avoid downloading 2 different images.
  ${ISTIO_BIN_BASE}/istio-iptables.sh "${@}"
  exit 0
fi

# Update iptables, based on config file
${ISTIO_BIN_BASE}/istio-iptables.sh

# Will run: ${ISTIO_BIN_BASE}/envoy -c $ENVOY_CFG --restart-epoch 0 --drain-time-s 2 --parent-shutdown-time-s 3 --service-cluster $SVC --service-node 'sidecar~${ISTIO_SVC_IP}~${POD_NAME}.${NS}.svc.cluster.local~${NS}.svc.cluster.local' $ISTIO_DEBUG >${ISTIO_LOG_DIR}/istio.log" istio-proxy
exec su -s /bin/bash -c "INSTANCE_IP=${ISTIO_SVC_IP} POD_NAME=${POD_NAME} POD_NAMESPACE=${NS} exec ${ISTIO_BIN_BASE}/pilot-agent proxy ${ISTIO_AGENT_FLAGS:-} --serviceCluster $SVC --discoveryAddress istio-pilot.${ISTIO_SYSTEM_NAMESPACE}:${ISTIO_PILOT_PORT} --controlPlaneAuthPolicy $CONTROL_PLANE_AUTH_POLICY 2> ${ISTIO_LOG_DIR}/istio.err.log > ${ISTIO_LOG_DIR}/istio.log" istio-proxy
