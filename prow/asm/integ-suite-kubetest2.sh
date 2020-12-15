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

# Setup junit report and verbose logging
export T="${T:-"-v"}"
export CI="true"

# shellcheck source=prow/asm/infra-lib.sh
source "${WD}/infra-lib.sh"

deployer_flags=("--up")

gke_deployer_flags=(
  "--ignore-gcp-ssh-key=true"
  "--gcp-service-account=${GOOGLE_APPLICATION_CREDENTIALS}"
  "-v=1"
)

DEPLOYER=""
EXTRA_DEPLOYER_FLAGS=""
EXTRA_GCLOUD_FLAGS=""
TEST_FLAGS=""
CLUSTER_TOPOLOGY="SINGLECLUSTER"
FEATURE_TO_TEST=""

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
    # Not all the gcloud command flags are supported by kubetest2 gke deployer.
    # If we want to pass some arbitrary gcloud flags that are currently not
    # supported, use this flag.
    # TODO(chizhg): This will not be needed after Tailorbird supports creating GKE on GCP clusters.
    --gke-deployer-gcloud-flags)
      EXTRA_GCLOUD_FLAGS=$2
      shift 2
    ;;
    --test-flags)
      TEST_FLAGS=$2
      shift 2
    ;;
    --topology)
      case $2 in
        SINGLECLUSTER | MULTICLUSTER | MULTIPROJECT_MULTICLUSTER )
          CLUSTER_TOPOLOGY=$2
          echo "Running with cluster topology ${CLUSTER_TOPOLOGY}"
          ;;
        *)
          echo "Error: Unsupported cluster topology ${CLUSTER_TOPOLOGY}" >&2
          exit 1
          ;;
      esac
      shift 2
    ;;
    --feature)
      case $2 in
        VPC_SC )
          FEATURE_TO_TEST=$2
          echo "Testing the feature ${FEATURE_TO_TEST}"
          ;;
        *)
          echo "Error: Unsupported feature for testing ${FEATURE_TO_TEST}" >&2
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

readonly DEPLOYER
readonly EXTRA_DEPLOYER_FLAGS
readonly EXTRA_GCLOUD_FLAGS
readonly TEST_FLAGS
readonly CLUSTER_TOPOLOGY
export DEPLOYER
export CLUSTER_TOPOLOGY

IFS=' ' read -r -a extra_deployer_flags <<< "$EXTRA_DEPLOYER_FLAGS"
deployer_flags+=( "${extra_deployer_flags[@]}" )

IFS=' ' read -r -a test_flags <<< "$TEST_FLAGS"

# Use kubetest2 gke deployer to provision GKE on GCP clusters.
if [[ "${DEPLOYER}" == "gke" ]]; then
  deployer_flags+=( "${gke_deployer_flags[@]}" )
  deployer_flags+=( "--create-command=beta container clusters create --quiet ${EXTRA_GCLOUD_FLAGS}" )
  if [[ "${CLUSTER_TOPOLOGY}" == "MULTICLUSTER"  ]]; then
    deployer_flags+=("--cluster-name=test1,test2" "--machine-type=e2-standard-4" "--num-nodes=3" "--region=us-central1")
    deployer_flags+=("--network=default" "--enable-workload-identity")
    if [[ "${FEATURE_TO_TEST}" == "VPC_SC" ]]; then
      gcp_project=$(vpc_sc_project_setup)
      deployer_flags+=("--project=${gcp_project}")
      deployer_flags+=("--private-cluster-access-level=limited")
      deployer_flags+=("--private-cluster-master-ip-range=173.16.0.32/28,172.16.0.32/28")
      deployer_flags+=("--create-command=beta container clusters create --quiet --master-authorized-networks=203.0.113.0/29")
    fi
  elif [[ "${CLUSTER_TOPOLOGY}" == "MULTIPROJECT_MULTICLUSTER" ]]; then
    # A slightly hacky step to setup the environment, see the comments on the
    # function signature.
    gcp_projects=$(multiproject_multicluster_setup)
    multi_cluster_deployer_flags+=("--cluster-name=prow-test1:1,prow-test2:2" "--machine-type=e2-standard-4" "--num-nodes=2" "--region=us-central1")
    multi_cluster_deployer_flags+=("--network=test-network" "--subnetwork-ranges=172.16.4.0/22 172.16.16.0/20 172.20.0.0/14,10.0.4.0/22 10.0.32.0/20 10.4.0.0/14" )
    multi_cluster_deployer_flags+=("--release-channel=regular" "--enable-workload-identity")
    # These projects are mananged by the boskos project rental pool in
    # https://gke-internal.googlesource.com/istio/test-infra-internal/+/refs/heads/master/boskos/config/resources.yaml#105
    multi_cluster_deployer_flags+=("--project=${gcp_projects}")
    deployer_flags+=( "${multi_cluster_deployer_flags[@]}" )
  elif [[ "${CLUSTER_TOPOLOGY}" == "SINGLECLUSTER" ]]; then
    deployer_flags+=("--cluster-name=test" "--machine-type=e2-standard-4" "--num-nodes=3" "--region=us-central1")
    deployer_flags+=("--network=default" "--enable-workload-identity")
  fi

# Use kubetest2 tailorbird deployer to provision multicloud clusters.
elif [[ "${DEPLOYER}" == "tailorbird" ]]; then
  echo "Installing Tailorbird CLI..."
  cookiefile="/secrets/cookiefile/cookies"
  git config --global http.cookiefile "${cookiefile}"
  git clone https://gke-internal.googlesource.com/test-infra "${GOPATH}"/src/gke-internal/test-infra
  pushd "${GOPATH}"/src/gke-internal/test-infra/
  go install "${GOPATH}"/src/gke-internal/test-infra/anthos/tailorbird/cmd/kubetest2-tailorbird
  popd
  rm -r "${GOPATH}"/src/gke-internal/test-infra

  echo "Installing herc CLI..."
  gsutil cp gs://anthos-hercules-public-artifacts/herc/latest/herc /usr/local/bin/ && chmod 755 /usr/local/bin/herc

  deployer_flags+=( "--down" )
  deployer_flags+=( "--tbenv=int" "--verbose" )
fi

# Run kubetest2 to start running the test workflow.
kubetest2 "${DEPLOYER}" "${deployer_flags[@]}" --test=exec -- "${WD}"/integ-run-tests.sh "${test_flags[@]}" || exit 1
