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


# Usage: ./integ-suite-kind.sh TARGET
# Example: ./integ-suite-kind.sh test.integration.pilot.kube.presubmit

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

while (( "$#" )); do
  case "$1" in
    # Node images can be found at https://github.com/kubernetes-sigs/kind/releases
    # For example, kindest/node:v1.14.0
    --node-image)
      NODE_IMAGE=$2
      shift 2
    ;;
    --skip-setup)
      SKIP_SETUP=true
      shift
    ;;
    --skip-cleanup)
      SKIP_CLEANUP=true
      shift
    ;;
    --skip-build)
      SKIP_BUILD=true
      shift
    ;;
    -*)
      echo "Error: Unsupported flag $1" >&2
      exit 1
      ;;
    *) # preserve positional arguments
      PARAMS+=("$1")
      shift
      ;;
  esac
done


# KinD will not have a LoadBalancer, so we need to disable it
export TEST_ENV=kind

# KinD will have the images loaded into it; it should not attempt to pull them
# See https://kind.sigs.k8s.io/docs/user/quick-start/#loading-an-image-into-your-cluster
export PULL_POLICY=IfNotPresent

export HUB=${HUB:-"istio-testing"}
export TAG="${TAG:-"istio-testing"}"

# Setup junit report and verbose logging
export T="${T:-"-v"}"
export CI="true"

make init

if [[ -z "${SKIP_SETUP:-}" ]]; then
  time setup_kind_cluster "${NODE_IMAGE:-}"
fi

if [[ -z "${SKIP_BUILD:-}" ]]; then
  time build_images
  time kind_load_images ""
fi

# If a variant is defined, update the tag accordingly
if [[ "${VARIANT:-}" != "" ]]; then
  export TAG="${TAG}-${VARIANT}"
fi

make "${PARAMS[*]}"
