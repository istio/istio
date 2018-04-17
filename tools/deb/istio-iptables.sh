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
# Initialization script responsible for setting up port forwarding for Istio sidecar.

# Based on pilot/docker/prepare_proxy.sh - but instead of capturing all traffic, only capture
# configured ranges.
# Compared to the K8S docker sidecar:
# - use config files - manual or pushed by an config system.
# - fine grain control over what inbound ports are captured
# - more control over what outbound traffic is captured
# - can be run multiple times, will cleanup previous rules
# - the "clean" option will remove all rules it previously added.

# After more testing, the goal is to replace and unify the script in K8S - by generating
# the sidecar image using the .deb file created by proxy.

function usage() {
  echo "${0} -p PORT -u UID [-h]"
  echo ''
  echo '  -p: Specify the envoy port to which redirect all TCP traffic (default $ENVOY_PORT = 150001)'
  echo '  -u: Specify the UID of the user for which the redirection is not'
  echo '      applied. Typically, this is the UID of the proxy container (default to uid of $ENVOY_USER, uid of istio_proxy, or 1337)'
  echo '  -i: Comma separated list of IP ranges in CIDR form to redirect to envoy (optional)'
  echo ''
  echo 'Using environment variables in $ISTIO_SIDECAR_CONFIG (default: /var/lib/istio/envoy/sidecar.env)'
}

set -o nounset
set -o pipefail
IFS=,

# The cluster env can be used for common cluster settings, pushed to all VMs in the cluster.
# This allows separating per-machine settings (the list of inbound ports, local path overrides) from cluster wide
# settings (CIDR range)
ISTIO_CLUSTER_CONFIG=${ISTIO_CLUSTER_CONFIG:-/var/lib/istio/envoy/cluster.env}
if [ -r ${ISTIO_CLUSTER_CONFIG} ]; then
  . ${ISTIO_CLUSTER_CONFIG}
fi

ISTIO_SIDECAR_CONFIG=${ISTIO_SIDECAR_CONFIG:-/var/lib/istio/envoy/sidecar.env}
if [ -r ${ISTIO_SIDECAR_CONFIG} ]; then
  . ${ISTIO_SIDECAR_CONFIG}
fi

# TODO: load all files from a directory, similar with ufw, to make it easier for automated install scripts
# Ideally we should generate ufw (and similar) configs as well, in case user already has an iptables solution.

IP_RANGES_INCLUDE=${ISTIO_SERVICE_CIDR:-}

while getopts ":p:u:e:i:h" opt; do
  case ${opt} in
    p)
      ENVOY_PORT=${OPTARG}
      ;;
    u)
      ENVOY_UID=${OPTARG}
      ;;
    i)
      IP_RANGES_INCLUDE=${OPTARG}
      ;;
    h)
      usage
      exit 0
      ;;
    \?)
      echo "Invalid option: -$OPTARG" >&2
      usage
      exit 1
      ;;
  esac
done


# TODO: more flexibility - maybe a whitelist of users to be captured for output instead of
# a blacklist.
if [ -z "${ENVOY_UID:-}" ]; then
  # Default to the UID of ENVOY_USER and root
  ENVOY_UID=$(id -u ${ENVOY_USER:-istio-proxy})
  if [ $? -ne 0 ]; then
     echo "Invalid istio user $ENVOY_UID $ENVOY_USER"
     exit 1
  fi
  # If ENVOY_UID is not explicitly defined (as it would be in k8s env), we add root to the list,
  # for ca agent.
  ENVOY_UID=${ENVOY_UID},0
fi

# Remove the old chains, to generate new configs.
iptables -t nat -D PREROUTING -p tcp -j ISTIO_INBOUND 2>/dev/null
iptables -t nat -D OUTPUT -p tcp -j ISTIO_OUTPUT 2>/dev/null

# Flush and delete the istio chains
iptables -t nat -F ISTIO_OUTPUT 2>/dev/null
iptables -t nat -X ISTIO_OUTPUT 2>/dev/null
iptables -t nat -F ISTIO_INBOUND 2>/dev/null
iptables -t nat -X ISTIO_INBOUND 2>/dev/null
iptables -t nat -F ISTIO_REDIRECT 2>/dev/null
iptables -t nat -X ISTIO_REDIRECT 2>/dev/null

if [ "${1:-}" = "clean" ]; then
  # Only cleanup, don't add new rules.
  exit 0
fi

# Create a new chain for redirecting inbound traffic to the common Envoy port.
# In the ISTIO_INBOUND and ISTIO_OUTBOUND chains, '-j RETURN' bypasses Envoy
# and '-j ISTIO_REDIRECT' redirects to Envoy.
iptables -t nat -N ISTIO_REDIRECT
iptables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-port ${ENVOY_PORT:-15001}

# Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
# to the local service. If not set, no inbound port will be intercepted by istio iptables.
if [ -n "${ISTIO_INBOUND_PORTS:-}" ]; then
  iptables -t nat -N ISTIO_INBOUND
  iptables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND

  # Makes sure SSH is not redirectred
  iptables -t nat -A ISTIO_INBOUND -p tcp --dport 22 -j RETURN

  if [ "${ISTIO_INBOUND_PORTS:-}" == "*" ]; then
       for port in ${ISTIO_LOCAL_EXCLUDE_PORTS:-}; do
          iptables -t nat -A ISTIO_INBOUND -p tcp --dport ${port} -j RETURN
       done
       iptables -t nat -A ISTIO_INBOUND -p tcp -j ISTIO_REDIRECT
  else
      for port in ${ISTIO_INBOUND_PORTS}; do
          iptables -t nat -A ISTIO_INBOUND -p tcp --dport ${port} -j ISTIO_REDIRECT
      done
  fi
fi

# TODO: change the default behavior to not intercept any output - user may use http_proxy or another
# iptables wrapper (like ufw). Current default is similar with 0.1

# Create a new chain for selectively redirecting outbound packets to Envoy.
iptables -t nat -N ISTIO_OUTPUT

# Jump to the ISTIO_OUTPUT chain from OUTPUT chain for all tcp traffic.
iptables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT

# Redirect app calls to back itself via Envoy when using the service VIP or endpoint
# address, e.g. appN => Envoy (client) => Envoy (server) => appN.
iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -j ISTIO_REDIRECT

for uid in ${ENVOY_UID}; do
  # Avoid infinite loops. Don't redirect Envoy traffic directly back to
  # Envoy for non-loopback traffic.
  iptables -t nat -A ISTIO_OUTPUT -m owner --uid-owner ${uid} -j RETURN
done

# Skip redirection for Envoy-aware applications and
# container-to-container traffic both of which explicitly use
# localhost.
iptables -t nat -A ISTIO_OUTPUT -d 127.0.0.1/32 -j RETURN

IFS=,
if [ -n "${IP_RANGES_INCLUDE:-}" ]; then
    for cidr in ${IP_RANGES_INCLUDE}; do
        iptables -t nat -A ISTIO_OUTPUT -d ${cidr} -j ISTIO_REDIRECT
    done
    iptables -t nat -A ISTIO_OUTPUT -j RETURN
else
    iptables -t nat -A ISTIO_OUTPUT -j ISTIO_REDIRECT
fi

