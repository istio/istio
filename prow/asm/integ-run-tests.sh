#!/bin/bash

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

echo "Using ${KUBECONFIG} to connect to the cluster(s)"

echo "Installing ASM control plane..."

echo "Running the e2e tests..."

echo "(Optional) Uninstalling ASM control plane..."
