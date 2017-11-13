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

WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

#######################################
# Presubmit script triggered by Prow. #
#######################################

# runs build and sets up variables
source ${ROOT}/prow/istio-common.sh
# Build
${ROOT}/bin/init.sh

echo 'Running Unit Tests'
time bazel test --test_output=all //...

# run linters in advisory mode
SKIP_INIT=1 ${ROOT}/bin/linters.sh

diff=`git diff`
if [[ -n "$diff" ]]; then
  echo "Some uncommitted changes are found. Maybe miss committing some generated files? Here's the diff"
  echo $diff
  exit -1
fi

# TODO define a job for codecov
echo "=== Code Coverage ==="
UPLOAD_TOKEN=istiocodecov.token
gsutil cp gs://istio-code-coverage/${UPLOAD_TOKEN} /tmp/${UPLOAD_TOKEN}
UPLOAD_TOKEN="@/tmp/${UPLOAD_TOKEN}" ${ROOT}/bin/codecov.sh

HUB="gcr.io/istio-testing"
TAG="${GIT_SHA}"
# upload images
time make push HUB="${HUB}" TAG="${TAG}"

time cd ${ROOT}/pilot; make e2etest HUB="${HUB}" TAG="${TAG}" TESTOPTS="-mixer=false"
