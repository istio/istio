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

# Usage:   ./integ-suite-kubetest2.sh --deployer [deployer_name]
#             --deployer-flags [deployer_flag1 deployer_flag2 ...] \
#             --test-flags [test_flag1 test_flag2 ...]
#
# Example: ./integ-suite-kubetest2.sh --deployer gke \
#             --deployer-flags "--project=test-project --cluster-name=test --region=us-central1"

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

echo "Installing kubetest2..."
# Switch to a temp directory to run `go get` which avoids touching the go.mod file.
temp_dir="$(mktemp -d)"
# Swallow the output as we are returning the stdout in the end.
pushd "${temp_dir}" > /dev/null 2>&1 || exit 1
GO111MODULE=on go get -u sigs.k8s.io/kubetest2
GO111MODULE=on go get -u sigs.k8s.io/kubetest2/kubetest2-gke
GO111MODULE=on go get -u sigs.k8s.io/kubetest2/kubetest2-kind
GO111MODULE=on go get -u sigs.k8s.io/kubetest2/kubetest2-tester-exec
# TODO(chizhg): install kubetest2-tailorbird
popd > /dev/null 2>&1 || exit 1

deployer_flags=( "--up" "--down" )

DEPLOYER=""
EXTRA_DEPLOYER_FLAGS=""
TEST_FLAGS=""

while (( "$#" )); do
  case "$1" in
    # kubetest2 deployer name, can be gke, tailorbird or kind
    --deployer)
      DEPLOYER=$2
      shift 2
    ;;
    # flags corresponding to the deployer being used, supported flags can be
    # checked by running `kubetest2 [deployer] --help`
    --deployer-flags)
      EXTRA_DEPLOYER_FLAGS=$2
      shift 2
    ;;
    --test-flags)
      TEST_FLAGS=$2
      shift 2
  esac
done

readonly DEPLOYER
readonly EXTRA_DEPLOYER_FLAGS
readonly TEST_FLAGS

IFS=' ' read -r -a extra_deployer_flags <<< "$EXTRA_DEPLOYER_FLAGS"
deployer_flags+=( "${extra_deployer_flags[@]}" )

IFS=' ' read -r -a test_flags <<< "$TEST_FLAGS"

# Run kubetest2 to start running the test workflow.
kubetest2 "${DEPLOYER}" "${deployer_flags[@]}" --test=exec -- "{WD}"/integ-run-tests.sh "${test_flags[@]}" || exit 1
