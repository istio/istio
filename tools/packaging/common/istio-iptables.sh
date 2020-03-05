#!/bin/bash
#
# Copyright 2017, 2018 Istio Authors. All Rights Reserved.
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

function usage() {
  echo "${0} -p PORT -u UID -g GID [-m mode] [-b ports] [-d ports] [-i CIDR] [-x CIDR] [-k interfaces] [-t] [-h]"
  echo ''
  # shellcheck disable=SC2016
  echo '  -p: Specify the envoy port to which redirect all TCP traffic (default $ENVOY_PORT = 15001)'
  # shellcheck disable=SC2016
  echo '  -z: Port to which all inbound TCP traffic to the pod/VM should be redirected to. For REDIRECT only (default $INBOUND_CAPTURE_PORT = 15006)'
  echo '  -u: Specify the UID of the user for which the redirection is not'
  echo '      applied. Typically, this is the UID of the proxy container'
  # shellcheck disable=SC2016
  echo '      (default to uid of $ENVOY_USER, uid of istio_proxy, or 1337)'
  echo '  -g: Specify the GID of the user for which the redirection is not'
  echo '      applied. (same default value as -u param)'
  echo '  -m: The mode used to redirect inbound connections to Envoy, either "REDIRECT" or "TPROXY"'
  # shellcheck disable=SC2016
  echo '      (default to $ISTIO_INBOUND_INTERCEPTION_MODE)'
  echo '  -b: Comma separated list of inbound ports for which traffic is to be redirected to Envoy (optional). The'
  echo '      wildcard character "*" can be used to configure redirection for all ports. An empty list will disable'
  # shellcheck disable=SC2016
  echo '      all inbound redirection (default to $ISTIO_INBOUND_PORTS)'
  echo '  -d: Comma separated list of inbound ports to be excluded from redirection to Envoy (optional). Only applies'
  # shellcheck disable=SC2016
  echo '      when all inbound traffic (i.e. "*") is being redirected (default to $ISTIO_LOCAL_EXCLUDE_PORTS)'
  echo '  -i: Comma separated list of IP ranges in CIDR form to redirect to envoy (optional). The wildcard'
  echo '      character "*" can be used to redirect all outbound traffic. An empty list will disable all outbound'
  # shellcheck disable=SC2016
  echo '      redirection (default to $ISTIO_SERVICE_CIDR)'
  echo '  -x: Comma separated list of IP ranges in CIDR form to be excluded from redirection. Only applies when all '
  # shellcheck disable=SC2016
  echo '      outbound traffic (i.e. "*") is being redirected (default to $ISTIO_SERVICE_EXCLUDE_CIDR).'
  echo '  -o: Comma separated list of outbound ports to be excluded from redirection to Envoy (optional).'
  echo '  -k: Comma separated list of virtual interfaces whose inbound traffic (from VM)'
  echo '      will be treated as outbound (optional)'
  echo '  -t: Unit testing, only functions are loaded and no other instructions are executed.'
  echo '  -h: Displays usage information and exits.'
  # shellcheck disable=SC2016
  echo ''
}

function dump {
    iptables-save
    ip6tables-save
}

function isValidIP() {
   if isIPv4 "${1}"; then
      true
   elif isIPv6 "${1}"; then
      true
   else
      false
   fi
}
#
# Function return true if argument is a valid ipv4 address
#
function isIPv4() {
   local ipv4regexp="^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$"
   if [[ ${1} =~ ${ipv4regexp} ]]; then
      true
   else
      false
  fi
}
#
# Function return true if argument is a valid ipv6 address
#
function isIPv6() {
  local ipv6section="^[0-9a-fA-F]{1,4}$"
  addr="$1"
  number_of_parts=0
  number_of_skip=0
  IFS=':' read -r -a addr <<< "$1"
  if [ ${#addr[@]} -eq 0 ]; then
     return 1
  fi
  for part in "${addr[@]}"; do
    # check to not exceed number of parts in ipv6 address
    if [[ ${number_of_parts} -ge 8 ]]; then
        return 1
    fi
    if [[ ${number_of_parts} -eq 0 ]] && ! [[ ${part} =~ ${ipv6section} ]]; then
        return 1
    fi
    if ! [[ ${part} =~ ${ipv6section} ]]; then
       if ! [[ "$part" == "" ]]; then
          return 1
       else
          # Found empty part, no more than 2 sections '::' are allowed in ipv6 address
          if [[ "$number_of_skip" -ge 1 ]]; then
             return 1
          fi
          ((number_of_skip++))
       fi
    fi
    ((number_of_parts++))
  done
  return 0
}

# Use a comma as the separator for multi-value arguments.
IFS=,

# TODO: load all files from a directory, similar with ufw, to make it easier for automated install scripts
# Ideally we should generate ufw (and similar) configs as well, in case user already has an iptables solution.

PROXY_PORT=${ENVOY_PORT:-15001}
PROXY_INBOUND_CAPTURE_PORT=${INBOUND_CAPTURE_PORT:-15006}
PROXY_UID=
PROXY_GID=
INBOUND_INTERCEPTION_MODE=${ISTIO_INBOUND_INTERCEPTION_MODE}
INBOUND_TPROXY_MARK=${ISTIO_INBOUND_TPROXY_MARK:-1337}
INBOUND_TPROXY_ROUTE_TABLE=${ISTIO_INBOUND_TPROXY_ROUTE_TABLE:-133}
INBOUND_PORTS_INCLUDE=${ISTIO_INBOUND_PORTS-}
INBOUND_PORTS_EXCLUDE=${ISTIO_LOCAL_EXCLUDE_PORTS-}
OUTBOUND_IP_RANGES_INCLUDE=${ISTIO_SERVICE_CIDR-}
OUTBOUND_IP_RANGES_EXCLUDE=${ISTIO_SERVICE_EXCLUDE_CIDR-}
OUTBOUND_PORTS_EXCLUDE=${ISTIO_LOCAL_OUTBOUND_PORTS_EXCLUDE-}
KUBEVIRT_INTERFACES=

while getopts ":p:z:u:g:m:b:d:o:i:x:k:ht" opt; do
  case ${opt} in
    p)
      PROXY_PORT=${OPTARG}
      ;;
    z)
      PROXY_INBOUND_CAPTURE_PORT=${OPTARG}
      ;;
    u)
      PROXY_UID=${OPTARG}
      ;;
    g)
      PROXY_GID=${OPTARG}
      ;;
    m)
      INBOUND_INTERCEPTION_MODE=${OPTARG}
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
    o)
      OUTBOUND_PORTS_EXCLUDE=${OPTARG}
      ;;
    k)
      KUBEVIRT_INTERFACES=${OPTARG}
      ;;
    t)
      echo "Unit testing is specified..."
      return
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

trap dump EXIT

# TODO: more flexibility - maybe a whitelist of users to be captured for output instead of a blacklist.
if [ -z "${PROXY_UID}" ]; then
  # Default to the UID of ENVOY_USER and root
  if ! PROXY_UID=$(id -u "${ENVOY_USER:-istio-proxy}"); then
     PROXY_UID="1337"
  fi
  # If ENVOY_UID is not explicitly defined (as it would be in k8s env), we add root to the list,
  # for ca agent.
  PROXY_UID=${PROXY_UID},0
fi
# for TPROXY as its uid and gid are same
if [ -z "${PROXY_GID}" ]; then
PROXY_GID=${PROXY_UID}
fi

POD_IP=$(hostname --ip-address)
# Check if pod's ip is ipv4 or ipv6, in case of ipv6 set variable
# to program ip6tables
if isIPv6 "$POD_IP"; then
  ENABLE_INBOUND_IPV6=$POD_IP
fi

#
# Since OUTBOUND_IP_RANGES_EXCLUDE could carry ipv4 and ipv6 ranges
# need to split them in different arrays one for ipv4 and one for ipv6
# in order to not to fail
pl='/*'
#
# Next two lines, read comma separated inclusion and exclusion lists into
# arrays, so each element could be validated individually.
#
IFS=',' read -ra EXCLUDE <<< "$OUTBOUND_IP_RANGES_EXCLUDE"
IFS=',' read -ra INCLUDE <<< "$OUTBOUND_IP_RANGES_INCLUDE"
ipv6_ranges_exclude=()
ipv4_ranges_exclude=()
for range in "${EXCLUDE[@]}"; do
    r=${range%$pl}
    if isValidIP "$r"; then
        if isIPv4 "$r"; then
            ipv4_ranges_exclude+=("$range")
        elif isIPv6 "$r"; then
            ipv6_ranges_exclude+=("$range")
        fi
    fi
done

ipv6_ranges_include=()
ipv4_ranges_include=()
if [ "${OUTBOUND_IP_RANGES_INCLUDE}" == "*" ]; then
   ipv6_ranges_include=("*")
   ipv4_ranges_include=("*")
else 
    for range in "${INCLUDE[@]}"; do
        r=${range%$pl}
        if isValidIP "$r"; then
            if isIPv4 "$r"; then
                ipv4_ranges_include+=("$range")
            elif isIPv6 "$r"; then
                ipv6_ranges_include+=("$range")
            fi
        fi
    done
fi

# Dump out our environment for debugging purposes.
echo "Environment:"
echo "------------"
echo "ENVOY_PORT=${ENVOY_PORT-}"
echo "INBOUND_CAPTURE_PORT=${INBOUND_CAPTURE_PORT-}"
echo "ISTIO_INBOUND_INTERCEPTION_MODE=${ISTIO_INBOUND_INTERCEPTION_MODE-}"
echo "ISTIO_INBOUND_TPROXY_MARK=${ISTIO_INBOUND_TPROXY_MARK-}"
echo "ISTIO_INBOUND_TPROXY_ROUTE_TABLE=${ISTIO_INBOUND_TPROXY_ROUTE_TABLE-}"
echo "ISTIO_INBOUND_PORTS=${ISTIO_INBOUND_PORTS-}"
echo "ISTIO_LOCAL_EXCLUDE_PORTS=${ISTIO_LOCAL_EXCLUDE_PORTS-}"
echo "ISTIO_SERVICE_CIDR=${ISTIO_SERVICE_CIDR-}"
echo "ISTIO_SERVICE_EXCLUDE_CIDR=${ISTIO_SERVICE_EXCLUDE_CIDR-}"
echo
echo "Variables:"
echo "----------"
echo "PROXY_PORT=${PROXY_PORT}"
echo "PROXY_INBOUND_CAPTURE_PORT=${PROXY_INBOUND_CAPTURE_PORT}"
echo "PROXY_UID=${PROXY_UID}"
echo "PROXY_GID=${PROXY_GID}"
echo "INBOUND_INTERCEPTION_MODE=${INBOUND_INTERCEPTION_MODE}"
echo "INBOUND_TPROXY_MARK=${INBOUND_TPROXY_MARK}"
echo "INBOUND_TPROXY_ROUTE_TABLE=${INBOUND_TPROXY_ROUTE_TABLE}"
echo "INBOUND_PORTS_INCLUDE=${INBOUND_PORTS_INCLUDE}"
echo "INBOUND_PORTS_EXCLUDE=${INBOUND_PORTS_EXCLUDE}"
echo "OUTBOUND_IP_RANGES_INCLUDE=${OUTBOUND_IP_RANGES_INCLUDE}"
echo "OUTBOUND_IP_RANGES_EXCLUDE=${OUTBOUND_IP_RANGES_EXCLUDE}"
echo "OUTBOUND_PORTS_EXCLUDE=${OUTBOUND_PORTS_EXCLUDE}"
echo "KUBEVIRT_INTERFACES=${KUBEVIRT_INTERFACES}"
echo "ENABLE_INBOUND_IPV6=${ENABLE_INBOUND_IPV6}"
echo


set +o nounset
# Blindly add a ipv6 address. If it fails it's fine.
# Add local ipv6 address to lo. Used in redirecting unknown ipv6 traffic to original dst.
# This address does not show up in neigh table so each Pod/Vm will only see its own. Think about 127.0.0.6.
if [ -n "${ENABLE_INBOUND_IPV6}" ]; then
  ip -6 addr add ::6/128 dev lo
fi

set -o errexit
set -o nounset
set -o pipefail
set -x # echo on

# Create a new chain for redirecting outbound traffic to the common Envoy port.
# In both chains, '-j RETURN' bypasses Envoy and '-j ISTIO_REDIRECT'
# redirects to Envoy.
iptables -t nat -N ISTIO_REDIRECT
iptables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-port "${PROXY_PORT}"

# Use this chain also for redirecting inbound traffic to the common Envoy port
# when not using TPROXY.
iptables -t nat -N ISTIO_IN_REDIRECT

# PROXY_INBOUND_CAPTURE_PORT should be used only user explicitly set INBOUND_PORTS_INCLUDE to capture all
if [ "${INBOUND_PORTS_INCLUDE}" == "*" ]; then
  iptables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port "${PROXY_INBOUND_CAPTURE_PORT}"
else
  iptables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port "${PROXY_PORT}"
fi

# Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
# to the local service. If not set, no inbound port will be intercepted by istio iptables.
if [ -n "${INBOUND_PORTS_INCLUDE}" ]; then
  if [ "${INBOUND_INTERCEPTION_MODE}" = "TPROXY" ] ; then
    # When using TPROXY, create a new chain for routing all inbound traffic to
    # Envoy. Any packet entering this chain gets marked with the ${INBOUND_TPROXY_MARK} mark,
    # so that they get routed to the loopback interface in order to get redirected to Envoy.
    # In the ISTIO_INBOUND chain, '-j ISTIO_DIVERT' reroutes to the loopback
    # interface.
    # Mark all inbound packets.
    iptables -t mangle -N ISTIO_DIVERT
    iptables -t mangle -A ISTIO_DIVERT -j MARK --set-mark "${INBOUND_TPROXY_MARK}"
    iptables -t mangle -A ISTIO_DIVERT -j ACCEPT

    # Route all packets marked in chain ISTIO_DIVERT using routing table ${INBOUND_TPROXY_ROUTE_TABLE}.
    ip -f inet rule add fwmark "${INBOUND_TPROXY_MARK}" lookup "${INBOUND_TPROXY_ROUTE_TABLE}"
    # In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
    # the loopback interface.
    ip -f inet route add local default dev lo table "${INBOUND_TPROXY_ROUTE_TABLE}" || ip route show table all

    # Create a new chain for redirecting inbound traffic to the common Envoy
    # port.
    # In the ISTIO_INBOUND chain, '-j RETURN' bypasses Envoy and
    # '-j ISTIO_TPROXY' redirects to Envoy.
    iptables -t mangle -N ISTIO_TPROXY
    iptables -t mangle -A ISTIO_TPROXY ! -d 127.0.0.1/32 -p tcp -j TPROXY --tproxy-mark "${INBOUND_TPROXY_MARK}/0xffffffff" --on-port "${PROXY_PORT}"

    table=mangle
  else
    table=nat
  fi
  iptables -t ${table} -N ISTIO_INBOUND
  iptables -t ${table} -A PREROUTING -p tcp -j ISTIO_INBOUND

  if [ "${INBOUND_PORTS_INCLUDE}" == "*" ]; then
    # Makes sure SSH is not redirected
    iptables -t ${table} -A ISTIO_INBOUND -p tcp --dport 22 -j RETURN
    # Apply any user-specified port exclusions.
    if [ -n "${INBOUND_PORTS_EXCLUDE}" ]; then
      for port in ${INBOUND_PORTS_EXCLUDE}; do
        iptables -t ${table} -A ISTIO_INBOUND -p tcp --dport "${port}" -j RETURN
      done
    fi
    # Redirect remaining inbound traffic to Envoy.
    if [ "${INBOUND_INTERCEPTION_MODE}" = "TPROXY" ]; then
      # If an inbound packet belongs to an established socket, route it to the
      # loopback interface.
      iptables -t mangle -A ISTIO_INBOUND -p tcp -m socket -j ISTIO_DIVERT || echo "No socket match support"
      # Otherwise, it's a new connection. Redirect it using TPROXY.
      iptables -t mangle -A ISTIO_INBOUND -p tcp -j ISTIO_TPROXY
    else
      iptables -t nat -A ISTIO_INBOUND -p tcp -j ISTIO_IN_REDIRECT
    fi
  else
    # User has specified a non-empty list of ports to be redirected to Envoy.
    for port in ${INBOUND_PORTS_INCLUDE}; do
      if [ "${INBOUND_INTERCEPTION_MODE}" = "TPROXY" ]; then
        iptables -t mangle -A ISTIO_INBOUND -p tcp --dport "${port}" -m socket -j ISTIO_DIVERT || echo "No socket match support"
        iptables -t mangle -A ISTIO_INBOUND -p tcp --dport "${port}" -m socket -j ISTIO_DIVERT || echo "No socket match support"
        iptables -t mangle -A ISTIO_INBOUND -p tcp --dport "${port}" -j ISTIO_TPROXY
      else
        iptables -t nat -A ISTIO_INBOUND -p tcp --dport "${port}" -j ISTIO_IN_REDIRECT
      fi
    done
  fi
fi

# TODO: change the default behavior to not intercept any output - user may use http_proxy or another
# iptables wrapper (like ufw). Current default is similar with 0.1

# Create a new chain for selectively redirecting outbound packets to Envoy.
iptables -t nat -N ISTIO_OUTPUT

# Jump to the ISTIO_OUTPUT chain from OUTPUT chain for all tcp traffic.
iptables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT

# Apply port based exclusions. Must be applied before connections back to self
# are redirected.
if [ -n "${OUTBOUND_PORTS_EXCLUDE}" ]; then
  for port in ${OUTBOUND_PORTS_EXCLUDE}; do
    iptables -t nat -A ISTIO_OUTPUT -p tcp --dport "${port}" -j RETURN
  done
fi

# 127.0.0.6 is bind connect from inbound passthrough cluster
iptables -t nat -A ISTIO_OUTPUT -o lo -s 127.0.0.6/32 -j RETURN

if [ -z "${DISABLE_REDIRECTION_ON_LOCAL_LOOPBACK-}" ]; then
  # Redirect app calls back to itself via Envoy when using the service VIP or endpoint
  # address, e.g. appN => Envoy (client) => Envoy (server) => appN.
  iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -j ISTIO_IN_REDIRECT
fi

for uid in ${PROXY_UID}; do
  # Avoid infinite loops. Don't redirect Envoy traffic directly back to
  # Envoy for non-loopback traffic.
  iptables -t nat -A ISTIO_OUTPUT -m owner --uid-owner "${uid}" -j RETURN
done

for gid in ${PROXY_GID}; do
  # Avoid infinite loops. Don't redirect Envoy traffic directly back to
  # Envoy for non-loopback traffic.
  iptables -t nat -A ISTIO_OUTPUT -m owner --gid-owner "${gid}" -j RETURN
done

# Skip redirection for Envoy-aware applications and
# container-to-container traffic both of which explicitly use
# localhost.
iptables -t nat -A ISTIO_OUTPUT -d 127.0.0.1/32 -j RETURN

# Apply outbound IPv4 exclusions. Must be applied before inclusions.
if [ ${#ipv4_ranges_exclude[@]} -gt 0 ]; then
  for cidr in "${ipv4_ranges_exclude[@]}"; do
    iptables -t nat -A ISTIO_OUTPUT -d "${cidr}" -j RETURN
  done
fi

for internalInterface in ${KUBEVIRT_INTERFACES}; do
    iptables -t nat -I PREROUTING 1 -i "${internalInterface}" -j RETURN
done

# Apply outbound IP inclusions.
if [ ${#ipv4_ranges_include[@]} -gt 0 ]; then
   if [ "${ipv4_ranges_include[0]}" == "*" ]; then
     # Wildcard specified. Redirect all remaining outbound traffic to Envoy.
     iptables -t nat -A ISTIO_OUTPUT -j ISTIO_REDIRECT
     for internalInterface in ${KUBEVIRT_INTERFACES}; do
       iptables -t nat -I PREROUTING 1 -i "${internalInterface}" -j ISTIO_REDIRECT
     done
   else 
     # User has specified a non-empty list of cidrs to be redirected to Envoy.
     for cidr in "${ipv4_ranges_include[@]}"; do
        for internalInterface in ${KUBEVIRT_INTERFACES}; do
           iptables -t nat -I PREROUTING 1 -i "${internalInterface}" -d "${cidr}" -j ISTIO_REDIRECT
        done
        iptables -t nat -A ISTIO_OUTPUT -d "${cidr}" -j ISTIO_REDIRECT
      done
      # All other traffic is not redirected.
      iptables -t nat -A ISTIO_OUTPUT -j RETURN
    fi
fi

# If ENABLE_INBOUND_IPV6 is unset (default unset), restrict IPv6 traffic.
set +o nounset
if [ -n "${ENABLE_INBOUND_IPV6}" ]; then
  # Create a new chain for redirecting outbound traffic to the common Envoy port.
  # In both chains, '-j RETURN' bypasses Envoy and '-j ISTIO_REDIRECT'
  # redirects to Envoy.
  ip6tables -t nat -N ISTIO_REDIRECT
  ip6tables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-port "${PROXY_PORT}"

  # Use this chain also for redirecting inbound traffic to the common Envoy port
  # when not using TPROXY.
  ip6tables -t nat -N ISTIO_IN_REDIRECT
  # PROXY_INBOUND_CAPTURE_PORT should be used only user explicitly set INBOUND_PORTS_INCLUDE to capture all
  if [ "${INBOUND_PORTS_INCLUDE}" == "*" ]; then
    ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port "${PROXY_INBOUND_CAPTURE_PORT}"
  else
    ip6tables -t nat -A ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port "${PROXY_PORT}"
  fi

  # Handling of inbound ports. Traffic will be redirected to Envoy, which will process and forward
  # to the local service. If not set, no inbound port will be intercepted by istio iptables.
  if [ -n "${INBOUND_PORTS_INCLUDE}" ]; then
    table=nat
    ip6tables -t ${table} -N ISTIO_INBOUND
    ip6tables -t ${table} -A PREROUTING -p tcp -j ISTIO_INBOUND

    if [ "${INBOUND_PORTS_INCLUDE}" == "*" ]; then
        # Makes sure SSH is not redirected
        ip6tables -t ${table} -A ISTIO_INBOUND -p tcp --dport 22 -j RETURN
        # Apply any user-specified port exclusions.
        if [ -n "${INBOUND_PORTS_EXCLUDE}" ]; then
        for port in ${INBOUND_PORTS_EXCLUDE}; do
            ip6tables -t ${table} -A ISTIO_INBOUND -p tcp --dport "${port}" -j RETURN
        done
        fi
    else
        # User has specified a non-empty list of ports to be redirected to Envoy.
        for port in ${INBOUND_PORTS_INCLUDE}; do
            ip6tables -t nat -A ISTIO_INBOUND -p tcp --dport "${port}" -j ISTIO_IN_REDIRECT
        done
    fi
  fi

  # Create a new chain for selectively redirecting outbound packets to Envoy.
  ip6tables -t nat -N ISTIO_OUTPUT

  # Jump to the ISTIO_OUTPUT chain from OUTPUT chain for all tcp traffic.
  ip6tables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT

  # Apply port based exclusions. Must be applied before connections back to self
  # are redirected.
  if [ -n "${OUTBOUND_PORTS_EXCLUDE}" ]; then
    for port in ${OUTBOUND_PORTS_EXCLUDE}; do
      ip6tables -t nat -A ISTIO_OUTPUT -p tcp --dport "${port}" -j RETURN
    done
  fi

  # ::6 is bind when connect from inbound passthrough cluster
  ip6tables -t nat -A ISTIO_OUTPUT -o lo -s ::6/128 -j RETURN

  # Redirect app calls to back itself via Envoy when using the service VIP or endpoint
  # address, e.g. appN => Envoy (client) => Envoy (server) => appN.
  ip6tables -t nat -A ISTIO_OUTPUT -o lo ! -d ::1/128 -j ISTIO_IN_REDIRECT

  for uid in ${PROXY_UID}; do
    # Avoid infinite loops. Don't redirect Envoy traffic directly back to
    # Envoy for non-loopback traffic.
    ip6tables -t nat -A ISTIO_OUTPUT -m owner --uid-owner "${uid}" -j RETURN
  done

  for gid in ${PROXY_GID}; do
    # Avoid infinite loops. Don't redirect Envoy traffic directly back to
    # Envoy for non-loopback traffic.
    ip6tables -t nat -A ISTIO_OUTPUT -m owner --gid-owner "${gid}" -j RETURN
  done

  # Skip redirection for Envoy-aware applications and
  # container-to-container traffic both of which explicitly use
  # localhost.
  ip6tables -t nat -A ISTIO_OUTPUT -d ::1/128 -j RETURN
  
  # Apply outbound IPv6 exclusions. Must be applied before inclusions.
  if [ ${#ipv6_ranges_exclude[@]} -gt 0 ]; then
    for cidr in "${ipv6_ranges_exclude[@]}"; do
      ip6tables -t nat -A ISTIO_OUTPUT -d "${cidr}" -j RETURN
    done
  fi
  # Apply outbound IPv6 inclusions.
  if [ ${#ipv6_ranges_include[@]} -gt 0 ]; then
     if [ "${ipv6_ranges_include[0]}" == "*" ]; then
       # Wildcard specified. Redirect all remaining outbound traffic to Envoy.
       ip6tables -t nat -A ISTIO_OUTPUT -j ISTIO_REDIRECT
       for internalInterface in ${KUBEVIRT_INTERFACES}; do
          ip6tables -t nat -I PREROUTING 1 -i "${internalInterface}" -j RETURN
       done
     else
       # User has specified a non-empty list of cidrs to be redirected to Envoy.
       for cidr in "${ipv6_ranges_include[@]}"; do
         for internalInterface in ${KUBEVIRT_INTERFACES}; do
           ip6tables -t nat -I PREROUTING 1 -i "${internalInterface}" -d "${cidr}" -j ISTIO_REDIRECT
         done
         ip6tables -t nat -A ISTIO_OUTPUT -d "${cidr}" -j ISTIO_REDIRECT
       done
       # All other traffic is not redirected.
       ip6tables -t nat -A ISTIO_OUTPUT -j RETURN
    fi
  fi
else
  # Drop all inbound traffic except established connections.
  ip6tables -F INPUT || true
  ip6tables -A INPUT -m state --state ESTABLISHED -j ACCEPT || true
  ip6tables -A INPUT -i lo -d ::1 -j ACCEPT || true
  ip6tables -A INPUT -j REJECT || true
fi
