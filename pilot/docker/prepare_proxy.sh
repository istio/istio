#!/bin/bash
# Proxy initialization script responsible for setting up port forwarding.

set -o errexit
set -o nounset
set -o pipefail

usage() {
  echo "${0} -p PORT -u UID [-h]"
  echo ''
  echo '  -p: Specify the proxy port to which redirect all TCP traffic'
  echo '  -u: Specify the UID of the user for which the redirection is not'
  echo '      applied. Typically, this is the UID of the proxy container'
  echo ''
}

while getopts ":p:u:h" opt; do
  case ${opt} in
    p)
      ISTIO_PROXY_PORT=${OPTARG}
      ;;
    u)
      ISTIO_PROXY_UID=${OPTARG}
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

if [[ -z "${ISTIO_PROXY_PORT-}" ]] || [[ -z "${ISTIO_PROXY_UID-}" ]]; then
  echo "Please set both -p and -u parameters"
  usage
  exit 1
fi

# 1. External traffic is redirected to proxy port
iptables -t nat -A PREROUTING -p tcp -j REDIRECT --to-port ${ISTIO_PROXY_PORT}

# 2. Locally generated traffic sent on loopback interface (because traffic was
# routed locally) and destination IP is not explicitly the loopback IP
# (e.g. 172.17.0.2 and not 127.0.0.1) is redirected back to proxy port.
# This include any proxy generated traffic which is necessary to make
# (app => proxy => proxy => app) work when two apps are same pod.
iptables -t nat -A OUTPUT -p tcp -o lo -j REDIRECT \
  ! -d 127.0.0.1/32 --to-port ${ISTIO_PROXY_PORT}

# 3. Locally generated traffic not sent explicitly from loopback IP
# (i.e. proxy aware) or not from the proxy itself is redirected proxy port.
iptables -t nat -A OUTPUT -p tcp -j REDIRECT ! -s 127.0.0.1/32 \
  --to-port ${ISTIO_PROXY_PORT} -m owner '!' --uid-owner ${ISTIO_PROXY_UID}

exit 0
