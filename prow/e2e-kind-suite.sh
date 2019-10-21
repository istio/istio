#!/bin/bash

# Copyright 2019 Istio Authors
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


#######################################################
# e2e-suite runs Istio E2E tests.                     #
#                                                     #
# Usage: ./e2e-kind-suite.sh --single_test mixer_e2e  #
#                                                     #
# ${E2E_ARGS} can be used to provide additional test  #
# arguments.                                          #
#######################################################

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
setup_and_export_git_sha

function load_kind_images() {
  for i in {1..3}; do
    # Archived local images and load it into KinD's docker daemon
    # Kubernetes in KinD can only access local images from its docker daemon.
    docker images "${HUB}/*:${TAG}" --format '{{.Repository}}:{{.Tag}}' | xargs -n1 kind --loglevel debug --name istio-testing load docker-image && break
    echo "Attempt ${i} to load images failed, retrying in 5s..."
    sleep 5
	done

    # If a variant is specified, load those images as well.
    # We should load non-variant images as well for things like `app` which do not use variants
    if [[ "${VARIANT:-}" != "" ]]; then
      for i in {1..3}; do
        docker images "${HUB}/*:${TAG}-${VARIANT}" --format '{{.Repository}}:{{.Tag}}' | xargs -n1 kind --loglevel debug --name istio-testing load docker-image
        echo "Attempt ${i} to load images failed, retrying in 5s..."
        sleep 5
	    done
    fi
}

function build_kind_images() {
  # Build just the images needed for the tests
  for image in pilot proxyv2 app test_policybackend mixer citadel galley sidecar_injector kubectl node-agent-k8s; do
    DOCKER_BUILD_VARIANTS="${VARIANT:-default}" make docker.${image}
  done

  time load_kind_images
}

# getopts only handles single character flags
for ((i=1; i<=$#; i++)); do
    case ${!i} in
        # Node images can be found at https://github.com/kubernetes-sigs/kind/releases
        # For example, kindest/node:v1.14.0
        --node-image)
          ((i++))
          NODE_IMAGE=${!i}
        ;;
        --skip-setup)
          SKIP_SETUP=true
          continue
        ;;
        --skip-build)
          SKIP_BUILD=true
          continue
        ;;
        --skip-cleanup)
          SKIP_CLEANUP=true
          continue
        ;;
        # -s/--single_test to specify test target to run.
        # e.g. "-s e2e_mixer" will trigger e2e mixer_test
        -s|--single_test) ((i++)); SINGLE_TEST=${!i}
        continue
        ;;
        --timeout) ((i++)); E2E_TIMEOUT=${!i}
        continue
        ;;
        --variant) ((i++)); VARIANT="${!i}"
        continue
        ;;
    esac
    E2E_ARGS+=( "${!i}" )
done


E2E_ARGS+=("--test_logs_path=${ARTIFACTS}")
# e2e tests with kind clusters on prow will get deleted when prow
# deleted the pod
E2E_ARGS+=("--skip_cleanup")
E2E_ARGS+=("--use_local_cluster")

# KinD will have the images loaded into it; it should not attempt to pull them
# See https://kind.sigs.k8s.io/docs/user/quick-start/#loading-an-image-into-your-cluster
E2E_ARGS+=("--image_pull_policy" "IfNotPresent")


export HUB=${HUB:-"istio-testing"}
export TAG="${TAG:-"istio-testing"}"

make init

if [[ -z "${SKIP_SETUP:-}" ]]; then
  time setup_kind_cluster "${NODE_IMAGE:-}"
fi

if [[ -z "${SKIP_BUILD:-}" ]]; then
  time build_kind_images
fi

if [[ "${ENABLE_ISTIO_CNI:-false}" == true ]]; then
   cni_run_daemon_kind
fi

time ISTIO_DOCKER_HUB=$HUB \
  E2E_ARGS="${E2E_ARGS[*]}" \
  JUNIT_E2E_XML="${ARTIFACTS}/junit.xml" \
  make with_junit_report TARGET="${SINGLE_TEST}" ${VARIANT:+ VARIANT="${VARIANT}"} ${E2E_TIMEOUT:+ E2E_TIMEOUT="${E2E_TIMEOUT}"}
