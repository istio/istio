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
  # shellcheck disable=SC1090
  . "$ISTIO_SIDECAR_CONFIG"
fi

# Set defaults
ISTIO_BIN_BASE=${ISTIO_BIN_BASE:-/usr/local/bin}
ISTIO_LOG_DIR=${ISTIO_LOG_DIR:-/var/log/istio}
ISTIO_CFG=${ISTIO_CFG:-/var/lib/istio}

# Default kube config for istio components
# TODO: use different configs, with different service accounts, for ca/pilot/mixer
KUBECONFIG=${ISTIO_CFG}/kube.config

# TODO: use separate user for ca
if [ "$(id -u)" = "0" ] ; then
    exec su -s /bin/bash -c "${ISTIO_BIN_BASE}/istio_ca --self-signed-ca --kube-config ${KUBECONFIG} 2> ${ISTIO_LOG_DIR}/istio_ca.err.log > ${ISTIO_LOG_DIR}/istio_ca.log" istio-proxy
else
    "${ISTIO_BIN_BASE}/istio_ca" --self-signed-ca --kube-config "${KUBECONFIG}"
fi
