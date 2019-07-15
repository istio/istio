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
setup_kind_cluster

# KinD will not have a LoadBalancer, so we need to disable it
export TEST_ENV=kind

# KinD will have the images loaded into it; it should not attempt to pull them
# See https://kind.sigs.k8s.io/docs/user/quick-start/#loading-an-image-into-your-cluster
export PULL_POLICY=IfNotPresent

export HUB=${HUB:-"kindtest"}
export TAG="${TAG:-${GIT_SHA}}"

make init
make docker

function load_kind_images(){
	# Archived local images and load it into KinD's docker daemon
	# Kubernetes in KinD can only access local images from its docker daemon.
	docker images "${HUB}/*:${TAG}" --format '{{.Repository}}:{{.Tag}}' | xargs -n1 -P16 kind --loglevel debug --name e2e-suite load docker-image
}

# In the CI the cluster will be using the same docker context as the docker images are built
# So we don't need to load the images. For local runs, this can be useful.
if [[ "${LOAD_IMAGES:-}" != "" ]]; then
  time load_kind_images
 fi

export JUNIT_UNIT_TEST_XML="${ARTIFACTS_DIR}/junit_unit-tests.xml"
export T="-v"
make "$@"
