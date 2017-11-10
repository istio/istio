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

DOCKER_IMAGE="istio-ca,istio-ca-test,node-agent-test"

ARGS="--image $DOCKER_IMAGE"

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

if [[ "$HUB" =~ ^gcr\.io ]]; then
  gcloud docker --authorize-only
fi

# Set certificate output directory
export CERTS_OUTPUT_DIR=`pwd`/docker/certs

# Generates certificate files
docker/certs/gen_certs.sh

# Build and push docker images
bin/push-docker -i $DOCKER_IMAGE -h $HUB -t $TAG

# Run integration tests
bazel run $BAZEL_ARGS //security/integration -- $ARGS \
-k $HOME/.kube/config \
--root-cert ${CERTS_OUTPUT_DIR}/istio_ca.crt \
--cert-chain ${CERTS_OUTPUT_DIR}/node_agent.crt \
--alsologtostderr
