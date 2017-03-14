#!/bin/bash

set -e 

# TODO Hardcoded to match user defined deployment/pod specs and istio proxy
# configuration. Optionally pull this from ConfigMap to dynamically coordinate
# uid and port management with proxy (re)start and config.
ISTIO_PROXY_PORT=5001
ISTIO_PROXY_UID=1337

iptables -t nat -A PREROUTING -p tcp -j REDIRECT --to-port $ISTIO_PROXY_PORT
iptables -t nat -A OUTPUT -p tcp -j REDIRECT ! -s 127.0.0.1/32 --to-port $ISTIO_PROXY_PORT -m owner '!' --uid-owner $ISTIO_PROXY_UID

# Enable core dumps to writable directory
sysctl -w  kernel.core_pattern=/tmp/core.%e.%p.%t

exit 0

