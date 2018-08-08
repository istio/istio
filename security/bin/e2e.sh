#!/bin/bash

# This script is a workaround due to inability to invoke bazel run targets from
# within bazel sandboxes. It is a simple shim over integration/main.go
# that accepts the same set of flags. Please add new flags to the Go test
# driver directly instead of extending this file.
#
# The additional steps that the script performs are:
# - set default docker tag based on a timestamp and user name
# - build and push docker images, including this repo pieces and proxy.

set -ex

SECURITY_ROOT="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && cd .. && pwd )"

ARGS=""
HUB=""
TAG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)
      TAG=$2
      shift
      ;;
    --hub)
      HUB=$2
      shift
      ;;
    *)
      ARGS="$ARGS $1"
  esac

  shift
done

if [[ -z $TAG ]]; then
  TAG=$(whoami)_$(date +%Y%m%d_%H%M%S)
fi
ARGS="$ARGS --tag $TAG"

if [[ -z $HUB ]]; then
  HUB="gcr.io/istio-testing"
fi
ARGS="$ARGS --hub $HUB"

if [[ -z $CERT_DIR ]]; then
  CERT_DIR=${SECURITY_ROOT}/docker
fi

# Run integration tests
go test -v istio.io/istio/security/tests/integration/certificateRotationTest "$ARGS"  \
-kube-config="$HOME/.kube/config"

go test -v istio.io/istio/security/tests/integration/secretCreationTest "$ARGS"  \
-kube-config="$HOME/.kube/config"

#See issue #3181 test below fails automated tests
#go test -v istio.io/istio/security/tests/integration/nodeAgentTest $ARGS  \
#-kube-config=$HOME/.kube/config \
#-root-cert=${CERT_DIR}/istio_ca.crt \
#-cert-chain=${CERT_DIR}/node_agent.crt
