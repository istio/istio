#!/bin/bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
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

WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

#######################################
# Presubmit script triggered by Prow. #
#######################################

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

if [ "${CI:-}" == 'bootstrap' ]; then
  # Test harness will checkout code to directory $GOPATH/src/github.com/istio/proxy
  # but we depend on being at path $GOPATH/src/istio.io/proxy for imports
  ln -sf ${GOPATH}/src/github.com/istio ${GOPATH}/src/istio.io
  ROOT=${GOPATH}/src/istio.io/proxy
  cd ${GOPATH}/src/istio.io/proxy

  # Setup bazel.rc
  cp "${ROOT}/tools/bazel.rc.ci" "${HOME}/.bazelrc"

  # Use the provided pull head sha, from prow.
  GIT_SHA="${PULL_PULL_SHA}"
else
  # Use the current commit.
  GIT_SHA="$(git rev-parse --verify HEAD)"
fi

cd $ROOT

export BAZEL_TEST_ARGS="--test_output=errors"

echo 'Bazel Tests'
make test_asan
