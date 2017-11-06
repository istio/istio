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

OUTPUT_DIR=`pwd`/docker

# Generate certificate and private key from root
echo 'Generate certificate and private key from root'
bazel run $BAZEL_ARGS //security/cmd/generate_cert -- \
-out-cert=${OUTPUT_DIR}/istio_ca.crt \
-out-priv=${OUTPUT_DIR}/istio_ca.key \
-organization="k8s.cluster.local" \
-self-signed=true \
-ca=true

# Generate certificate and private key from istio_ca
bazel run $BAZEL_ARGS //security/cmd/generate_cert -- \
-out-cert=${OUTPUT_DIR}/node_agent.crt \
-out-priv=${OUTPUT_DIR}/node_agent.key \
-organization="NodeAgent" \
-host="nodeagent.google.com" \
-signer-cert=${OUTPUT_DIR}/istio_ca.crt \
-signer-priv=${OUTPUT_DIR}/istio_ca.key

# Build and push docker images
bin/push-docker -i $DOCKER_IMAGE -h $HUB -t $TAG

# Run integration tests
bazel run $BAZEL_ARGS //security/integration -- $ARGS \
-k $HOME/.kube/config \
--root-cert ${OUTPUT_DIR}/istio_ca.crt \
--cert-chain ${OUTPUT_DIR}/istio_ca.crt \
--alsologtostderr
