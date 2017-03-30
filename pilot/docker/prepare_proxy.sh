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

iptables -t nat -A PREROUTING -p tcp -j REDIRECT --to-port ${ISTIO_PROXY_PORT}
iptables -t nat -A OUTPUT -p tcp -j REDIRECT ! -s 127.0.0.1/32 \
  --to-port ${ISTIO_PROXY_PORT} -m owner '!' --uid-owner ${ISTIO_PROXY_UID}

exit 0
