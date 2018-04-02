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

set -x # echo on

function usage() {
  echo "${0} -p PORT -u UID [-b ports] [-d ports] [-i CIDR] [-x CIDR] [-h]"
  echo ''
  echo '  -p: Specify the envoy port to which redirect all TCP traffic (default $ENVOY_PORT = 15001)'
  echo '  -u: Specify the UID of the user for which the redirection is not'
  echo '      applied. Typically, this is the UID of the proxy container'
  echo '      (default to uid of $ENVOY_USER, uid of istio_proxy, or 1337)'
  echo '  -b: Comma separated list of inbound ports for which traffic is to be redirected to Envoy (optional). The'
  echo '      wildcard character "*" can be used to configure redirection for all ports. An empty list will disable'
  echo '      all inbound redirection (default to $ISTIO_INBOUND_PORTS or "*")'
  echo '  -d: Comma separated list of inbound ports to be excluded from redirection to Envoy (optional). Only applies'
  echo '      when all inbound traffic (i.e. "*") is being redirected (default to $ISTIO_LOCAL_EXCLUDE_PORTS)'
  echo '  -i: Comma separated list of IP ranges in CIDR form to redirect to envoy (optional). The wildcard'
  echo '      character "*" can be used to redirect all outbound traffic. An empty list will disable all outbound'
  echo '      redirection (default to $ISTIO_SERVICE_CIDR or "*")'
  echo '  -x: Comma separated list of IP ranges in CIDR form to be excluded from redirection. Only applies when all '
  echo '      outbound traffic (i.e. "*") is being redirected (default to $ISTIO_SERVICE_EXCLUDE_CIDR).'
  echo ''
  echo 'Using environment variables in $ISTIO_SIDECAR_CONFIG (default: /var/lib/istio/envoy/sidecar.env)'
}

# Use a comma as the separator for multi-value arguments.
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

PROXY_PORT=${ENVOY_PORT:-15001}
PROXY_UID=
INBOUND_PORTS_INCLUDE=${ISTIO_INBOUND_PORTS-"*"}
INBOUND_PORTS_EXCLUDE=${ISTIO_LOCAL_EXCLUDE_PORTS-}
OUTBOUND_IP_RANGES_INCLUDE=${ISTIO_SERVICE_CIDR-"*"}
OUTBOUND_IP_RANGES_EXCLUDE=${ISTIO_SERVICE_EXCLUDE_CIDR-}

while getopts ":p:u:b:d:i:x:h" opt; do
  case ${opt} in
    p)
      PROXY_PORT=${OPTARG}
      ;;
    u)
      PROXY_UID=${OPTARG}
      ;;
    b)
      INBOUND_PORTS_INCLUDE=${OPTARG}
      ;;
    d)
      INBOUND_PORTS_EXCLUDE=${OPTARG}
      ;;
    i)
      OUTBOUND_IP_RANGES_INCLUDE=${OPTARG}
      ;;
    x)
      OUTBOUND_IP_RANGES_EXCLUDE=${OPTARG}
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

# TODO: more flexibility - maybe a whitelist of users to be captured for output instead of a blacklist.
if [ -z "${PROXY_UID}" ]; then
  # Default to the UID of ENVOY_USER and root
  PROXY_UID=$(id -u ${ENVOY_USER:-istio-proxy})
  if [ $? -ne 0 ]; then
     echo "Invalid istio user $PROXY_UID $ENVOY_USER"
     exit 1
  fi
  # If ENVOY_UID is not explicitly defined (as it would be in k8s env), we add root to the list,
  # for ca agent.
  PROXY_UID=${PROXY_UID},0
fi

# Remove the old chains, to generate new configs.
iptables -t nat -D PREROUTING -p tcp -j ISTIO_INBOUND
iptables -t nat -D OUTPUT -p tcp -j ISTIO_OUTPUT

# Flush and delete the istio chains
iptables -t nat -F ISTIO_OUTPUT
iptables -t nat -X ISTIO_OUTPUT
iptables -t nat -F ISTIO_INBOUND
iptables -t nat -X ISTIO_INBOUND
iptables -t nat -F ISTIO_REDIRECT
iptables -t nat -X ISTIO_REDIRECT

if [ "${1:-}" = "clean" ]; then
  # Only cleanup, don't add new rules.
  exit 0
fi

set -o errexit
set -o nounset
set -o pipefail

# Create a new chain for redirecting inbound traffic to the common Envoy port.
# In the ISTIO_INBOUND and ISTIO_OUTBOUND chains, '-j RETURN' bypasses Envoy
# and '-j ISTIO_REDIRECT' redirects to Envoy.
iptables -t nat -N ISTIO_REDIRECT
iptables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-port ${PROXY_PORT}

# Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
# to the local service. If not set, no inbound port will be intercepted by istio iptables.
if [ -n "${INBOUND_PORTS_INCLUDE}" ]; then
  iptables -t nat -N ISTIO_INBOUND
  iptables -t nat -A PREROUTING -p tcp -j ISTIO_INBOUND

  if [ "${INBOUND_PORTS_INCLUDE}" == "*" ]; then
    # Makes sure SSH is not redirected
    iptables -t nat -A ISTIO_INBOUND -p tcp --dport 22 -j RETURN
    # Apply any user-specified port exclusions.
    if [ -n "${INBOUND_PORTS_EXCLUDE}" ]; then
      for port in ${INBOUND_PORTS_EXCLUDE}; do
        iptables -t nat -A ISTIO_INBOUND -p tcp --dport ${port} -j RETURN
      done
    fi
    # Redirect remaining inbound traffic to Envoy.
    iptables -t nat -A ISTIO_INBOUND -p tcp -j ISTIO_REDIRECT
  else
    # User has specified a non-empty list of ports to be redirected to Envoy.
    for port in ${INBOUND_PORTS_INCLUDE}; do
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

for uid in ${PROXY_UID}; do
  # Avoid infinite loops. Don't redirect Envoy traffic directly back to
  # Envoy for non-loopback traffic.
  iptables -t nat -A ISTIO_OUTPUT -m owner --uid-owner ${uid} -j RETURN
done

# Skip redirection for Envoy-aware applications and
# container-to-container traffic both of which explicitly use
# localhost.
iptables -t nat -A ISTIO_OUTPUT -d 127.0.0.1/32 -j RETURN

if [ -n "${OUTBOUND_IP_RANGES_INCLUDE}" ]; then
  if [ "${OUTBOUND_IP_RANGES_INCLUDE}" == "*" ]; then
    # Redirect exclusions must be applied before inclusions.
    if [ -n "${OUTBOUND_IP_RANGES_EXCLUDE}" ]; then
      for cidr in ${OUTBOUND_IP_RANGES_EXCLUDE}; do
        iptables -t nat -A ISTIO_OUTPUT -d ${cidr} -j RETURN
      done
    fi
    # Redirect remaining outbound traffic to Envoy
    iptables -t nat -A ISTIO_OUTPUT -j ISTIO_REDIRECT
  else
    # User has specified a non-empty list of cidrs to be redirected to Envoy.
    for cidr in ${OUTBOUND_IP_RANGES_INCLUDE}; do
      iptables -t nat -A ISTIO_OUTPUT -d ${cidr} -j ISTIO_REDIRECT
    done
    # All other traffic is not redirected.
    iptables -t nat -A ISTIO_OUTPUT -j RETURN
  fi
fi
