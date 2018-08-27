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
# Script to configure and start the Istio node agent.
# Will run the node_agent as istio-proxy instead of root - to allow interception
# of apps running as root (node_agent requires root to not be intercepted) and
# to reduce risks.

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

EXEC_USER=${EXEC_USER:-istio-proxy}

if [ -z "${CITADEL_ADDRESS:-}" ]; then
  CITADEL_ADDRESS=istio-citadel:8060
fi

CERTS_DIR=${CERTS_DIR:-/etc/certs}

CITADEL_ARGS="--ca-address ${CITADEL_ADDRESS}"
CITADEL_ARGS="${CITADEL_ARGS} --cert-chain ${CERTS_DIR}/cert-chain.pem"
CITADEL_ARGS="${CITADEL_ARGS} --key ${CERTS_DIR}/key.pem"
CITADEL_ARGS="${CITADEL_ARGS} --root-cert ${CERTS_DIR}/root-cert.pem"

if [ -z "${CITADEL_ENV:-}" ]; then
  CITADEL_ARGS="${CITADEL_ARGS} --env onprem"
else
  CITADEL_ARGS="${CITADEL_ARGS} --env ${CITADEL_ENV}"
fi

if [ ${EXEC_USER} == ${USER:-} ] ; then
  ${ISTIO_BIN_BASE}/node_agent ${CITADEL_ARGS}
else
  su -s /bin/sh -c "exec ${ISTIO_BIN_BASE}/node_agent ${CITADEL_ARGS}" ${EXEC_USER}
fi