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

# shellcheck source=prow/asm/lib.sh
source "${WD}/lib.sh"

echo "Installing kubetest2..."
# Switch to a temp directory to run `go get` which avoids touching the go.mod file.
temp_dir="$(mktemp -d)"
# Swallow the output as we are returning the stdout in the end.
pushd "${temp_dir}" > /dev/null 2>&1 || exit 1
GO111MODULE=on go get -u sigs.k8s.io/boskos/cmd/boskosctl
GO111MODULE=on go get -u sigs.k8s.io/kubetest2
GO111MODULE=on go get -u sigs.k8s.io/kubetest2/kubetest2-gke
GO111MODULE=on go get -u sigs.k8s.io/kubetest2/kubetest2-kind
GO111MODULE=on go get -u sigs.k8s.io/kubetest2/kubetest2-tester-exec
# TODO(chizhg): install kubetest2-tailorbird
popd > /dev/null 2>&1 || exit 1

deployer_flags=(
  "--up" "--create-command=beta container clusters create --quiet" "--ignore-gcp-ssh-key=true"
  "--gcp-service-account=${GOOGLE_APPLICATION_CREDENTIALS}"
)

DEPLOYER=""
EXTRA_DEPLOYER_FLAGS=""
TEST_FLAGS=""
TOPOLOGY=SINGLE_CLUSTER

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
    ;;
    --topology)
      case $2 in
        MULTICLUSTER | MULTIPROJECT_MULTICLUSTER )
          TOPOLOGY=$2
          echo "Running with topology ${TOPOLOGY}"
          ;;
        *)
          echo "Error: Unsupported topology ${TOPOLOGY}" >&2
          exit 1
          ;;
      esac
      shift 2
    ;;
    *)
      echo "Error: unknown option $1"
      exit 1
      ;;
  esac
done

# Activate the service account with the key file.
# gcloud auth activate-service-account --key-file="${GOOGLE_APPLICATION_CREDENTIALS}"

readonly DEPLOYER
readonly EXTRA_DEPLOYER_FLAGS
readonly TEST_FLAGS
readonly TOPOLOGY
export DEPLOYER
export TOPOLOGY

IFS=' ' read -r -a extra_deployer_flags <<< "$EXTRA_DEPLOYER_FLAGS"
deployer_flags+=( "${extra_deployer_flags[@]}" )

IFS=' ' read -r -a test_flags <<< "$TEST_FLAGS"

if [[ "${DEPLOYER}" == "gke" ]]; then
  if [[ "${TOPOLOGY}" == "MULTICLUSTER"  ]]; then
    deployer_flags+=("--cluster-name=test1,test2" "--machine-type=e2-standard-4" "--num-nodes=3" "--region=us-central1")
  elif [[ "${TOPOLOGY}" == "MULTIPROJECT_MULTICLUSTER" ]]; then
    # A slightly hacky step to setup the environment, see the comments on the
    # function signature.
    multiproject_multicluster_setup
    multi_cluster_deployer_flags=("--cluster-name=prow-test1:1,prow-test2:2" "--machine-type=e2-standard-4" "--num-nodes=1" "--region=us-central1")
    multi_cluster_deployer_flags+=("--network=test-network" "--subnetwork-ranges=172.16.4.0/22 172.16.16.0/20 172.20.0.0/14,10.0.4.0/22 10.0.32.0/20 10.4.0.0/14" )
    # These projects are mananged by the boskos project rental pool in
    # https://gke-internal.googlesource.com/istio/test-infra-internal/+/refs/heads/master/boskos/config/resources.yaml#105
    multi_cluster_deployer_flags+=("--project=engprod-test-host,engprod-test-service-1,engprod-test-service-2")
    deployer_flags+=( "${multi_cluster_deployer_flags[@]}" )
  fi
fi

# Run kubetest2 to start running the test workflow.
kubetest2 "${DEPLOYER}" "${deployer_flags[@]}" --test=exec -- "${WD}"/integ-run-tests.sh "${test_flags[@]}" || exit 1
