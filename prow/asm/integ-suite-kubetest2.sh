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

# This script is used as the entrypoint for running Prow jobs.
# It brings up the Kubernetes clusters based on the input flags, and then invokes
# integ-run-tests.sh which will setup the SUT and run the tests.

# Usage:   ./integ-suite-kubetest2.sh --cluster-type [cluster_type]
#             --deployer-flags [deployer_flag1 deployer_flag2 ...] \
#             --test-flags [test_flag1 test_flag2 ...]
#
# Example: ./integ-suite-kubetest2.sh --cluster-type gke \
#             --deployer-flags "--project=test-project --cluster-name=test --region=us-central1"

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

CURTDIR="$(pwd)"
cd "${CURTDIR}/prow/asm/tester" && go install . && cd -
cd ./prow/asm/infra && go run ./main.go --repo-root-dir="${CURTDIR}" \
    --test-script="tester" "$@"
