#!/bin/bash

# Delete any previous kind cluster
kind delete

echo "Starting Kind."

# Start kind
kind create

# Set KubeConfig
mkdir -p "$HOME/.kube"
sudo mv "$HOME/.kube/kind-config-1" "$HOME/.kube/config"
sudo chown "$(id -u)":"$(id -g)" "$HOME/.kube/config"

# Set up env ISTIO if not done yet
if [[ -z "${ISTIO// }" ]]; then
  if [[ -z "${GOPATH// }" ]]; then
    echo GOPATH is not set. Please set and run script again.
    exit
  fi
  export ISTIO=$GOPATH/src/istio.io
  echo 'Set ISTIO to' "$ISTIO"
fi

echo "Host Setup Completed"
