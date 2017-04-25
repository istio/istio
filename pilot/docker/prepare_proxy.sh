#!/bin/bash
# Envoy initialization script responsible for setting up port forwarding.

set -o errexit
set -o nounset
set -o pipefail

usage() {
  echo "${0} -p PORT -u UID [-h]"
  echo ''
  echo '  -p: Specify the envoy port to which redirect all TCP traffic'
  echo '  -u: Specify the UID of the user for which the redirection is not'
  echo '      applied. Typically, this is the UID of the proxy container'
  echo '  -i: Comma separated list of IP ranges in CIDR form to redirect to envoy (optional)'
  echo ''
}

IP_RANGES_INCLUDE=""

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

if [[ -z "${ENVOY_PORT-}" ]] || [[ -z "${ENVOY_UID-}" ]]; then
  echo "Please set both -p and -u parameters"
  usage
  exit 1
fi

# Create a new chain for redirecting inbound and outbound traffic to
# the common Envoy port.
iptables -t nat -N ISTIO_REDIRECT                                             -m comment --comment "istio/redirect-common-chain"
iptables -t nat -A ISTIO_REDIRECT -p tcp -j REDIRECT --to-port ${ENVOY_PORT}  -m comment --comment "istio/redirect-to-envoy-port"

# Redirect all inbound traffic to Envoy.
iptables -t nat -A PREROUTING -j ISTIO_REDIRECT                               -m comment --comment "istio/install-istio-prerouting"

# Create a new chain for selectively redirecting outbound packets to
# Envoy.
iptables -t nat -N ISTIO_OUTPUT                                               -m comment --comment "istio/common-output-chain"

# Jump to the ISTIO_OUTPUT chain from OUTPUT chain for all tcp
# traffic. '-j RETURN' bypasses Envoy and '-j ISTIO_REDIRECT'
# redirects to Envoy.
iptables -t nat -A OUTPUT -p tcp -j ISTIO_OUTPUT                              -m comment --comment "istio/install-istio-output"

# Redirect app calls to back itself via Envoy when using the service VIP or endpoint
# address, e.g. appN => Envoy (client) => Envoy (server) => appN.
iptables -t nat -A ISTIO_OUTPUT -o lo ! -d 127.0.0.1/32 -j ISTIO_REDIRECT     -m comment --comment "istio/redirect-implicit-loopback"

# Avoid infinite loops. Don't redirect Envoy traffic directly back to
# Envoy for non-loopback traffic.
iptables -t nat -A ISTIO_OUTPUT -m owner --uid-owner ${ENVOY_UID} -j RETURN   -m comment --comment "istio/bypass-envoy"

# Skip redirection for Envoy-aware applications and
# container-to-container traffic both of which explicitly use
# localhost.
iptables -t nat -A ISTIO_OUTPUT -d 127.0.0.1/32 -j RETURN                     -m comment --comment "istio/bypass-explicit-loopback"

# All outbound traffic will be redirected to Envoy by default. If
# IP_RANGES_INCLUDE is non-empty, only traffic bound for the
# destinations specified in this list will be captured.
IFS=,
if [ "${IP_RANGES_INCLUDE}" != "" ]; then
    for cidr in ${IP_RANGES_INCLUDE}; do
        iptables -t nat -A ISTIO_OUTPUT -d ${cidr} -j ISTIO_REDIRECT          -m comment --comment "istio/redirect-ip-range-${cidr}"
    done
    iptables -t nat -A ISTIO_OUTPUT -j RETURN                                 -m comment --comment "istio/bypass-default-outbound"
else
    iptables -t nat -A ISTIO_OUTPUT -j ISTIO_REDIRECT                         -m comment --comment "istio/redirect-default-outbound"
fi

exit 0
