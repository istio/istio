#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.


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
  # Test harness will checkout code to directory $GOPATH/src/github.com/istio/istio
  # but we depend on being at path $GOPATH/src/istio.io/istio for imports
  ln -sf ${GOPATH}/src/github.com/istio ${GOPATH}/src/istio.io
  cd ${GOPATH}/src/istio.io/istio

  # Use the provided pull head sha, from prow.
  GIT_SHA="${PULL_PULL_SHA}"
else
  # Use the current commit.
  GIT_SHA="$(git rev-parse --verify HEAD)"
fi

echo 'Running Linters'
./bin/linters.sh

echo 'Running Unit Tests'
bazel test --test_output=all //...

echo 'Checking that updateVersion has been called'
install/updateVersion.sh -s

echo 'Pushing Images'
(cd devel/fortio && make authorize all TAG="${GIT_SHA}")
