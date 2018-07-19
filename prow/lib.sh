#!/bin/bash

# Copyright 2018 Istio Authors

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

function setup_and_export_git_sha() {
  if [ "${CI:-}" == 'bootstrap' ]; then
    # Handle prow environment and checkout
    export USER=Prow

    # Make sure we are in the right directory
    # Test harness will checkout code to directory $GOPATH/src/github.com/istio/istio
    # but we depend on being at path $GOPATH/src/istio.io/istio for imports
    if [[ ! $PWD = ${GOPATH}/src/istio.io/istio ]]; then
      mv ${GOPATH}/src/github.com/${REPO_OWNER:-istio} ${GOPATH}/src/istio.io
      export ROOT=${GOPATH}/src/istio.io/istio
      cd ${GOPATH}/src/istio.io/istio
    fi

    if [ -z "${PULL_PULL_SHA:-}" ]; then
      export GIT_SHA="${PULL_BASE_SHA}"
    else
      export GIT_SHA="${PULL_PULL_SHA}"
    fi

    # Use volume mount from pilot-presubmit job's pod spec.
    export KUBECONFIG="${HOME}/.kube/config"

    # Set artifact dir based on checkout
    export ARTIFACTS_DIR="${GOPATH}/src/istio.io/istio/_artifacts"
  else
    # Use the current commit.
    export GIT_SHA="$(git rev-parse --verify HEAD)"
  fi
  gcloud auth configure-docker -q
}
