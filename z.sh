#!/bin/bash
set -euo pipefail

COMPONENTS='operator istioctl galley mixer_codegen mixer test_policybackend app_sidecar app proxyv2 pilot'
TAG=1534c5c92311189959b6f971aa2911aae0ea1332
DOCKER_HUP_REPO=docker.io/xunholy

for COMPONENT in $COMPONENTS; do
    echo "$DOCKER_HUP_REPO/$COMPONENT:$TAG"
    docker tag "istio/$COMPONENT:$TAG" "$DOCKER_HUP_REPO/$COMPONENT:$TAG"
    docker push "$DOCKER_HUP_REPO/$COMPONENT:$TAG"
done
