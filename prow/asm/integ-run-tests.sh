#!/bin/bash

# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script is mainly responsible for setting up the SUT and running the tests.
# The env vars used here are set by the integ-suite-kubetest2.sh script, which
# is the entrypoint for the test jobs run by Prow.

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

echo "Using ${KUBECONFIG} to connect to the cluster(s)"

echo "The kubetest2 deployer is ${DEPLOYER}"

echo "The topology is ${CLUSTER_TOPOLOGY}"

echo "Installing ASM control plane..."

echo "Running the e2e tests..."

echo "(Optional) Uninstalling ASM control plane..."
