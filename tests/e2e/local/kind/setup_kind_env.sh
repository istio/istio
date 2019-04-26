#!/bin/bash

# Set up env ISTIO if not done yet
if [[ -z "${ISTIO// }" ]]; then
  if [[ -z "${GOPATH// }" ]]; then
    echo GOPATH is not set. Please set and run script again.
    exit
  fi
  export ISTIO=$GOPATH/src/istio.io
  echo 'Set ISTIO to' "$ISTIO"
fi

# Delete any previous e2e KinD cluster
echo "Deleting previous KinD cluster with name=e2e..."
kind delete cluster --name=e2e &> /dev/null || true
echo "Done deleting previous e2e cluster."

# Create KinD
echo "Creating KinD environment..."
if ! (kind create cluster --name=e2e) > /dev/null; then
	echo "Could not setup KinD environment. Something wrong with KinD setup. Please check your setup and try again."
	exit 1
fi

KUBECONFIG=$(kind get kubeconfig-path --name="e2e")
export KUBECONFIG

echo """
KinD environment is setup for this shell. Use:

KUBECONFIG=$(kind get kubeconfig-path --name="e2e")
export KUBECONFIG
"""