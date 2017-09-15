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
    # Test harness will checkout code to directory $GOPATH/src/github.com/istio
    # but we depend on being at path $GOPATH/src/istio.io for imports.
    mv ${GOPATH}/src/github.com/istio ${GOPATH}/src/istio.io
    cd ${GOPATH}/src/istio.io/broker

    # Use the provided pull head sha, from prow.
    GIT_SHA="${PULL_PULL_SHA}"

    # Use volume mount from broker-presubmit job's pod spec.
    ln -sf "${HOME}/.kube/config" pkg/platform/kube/config
else
    # Use the current commit.
    GIT_SHA="$(git rev-parse --verify HEAD)"
fi

echo "=== Bazel Build ==="
make build

echo "=== Bazel Tests ==="
make test

# TODO temporary turn off
# echo "=== Code Check ==="
#export LAST_GOOD_GITSHA="${PULL_PULL_SHA}"
# make lint

#echo "=== Code Coverage ==="
#make coverage | tee codecov.report
#if [ "${CI:-}" == "bootstrap" ]; then
#    BUILD_ID="PROW-${BUILD_NUMBER}" JOB_NAME="broker/presubmit" ./bin/toolbox/presubmit/pkg_coverage.sh
#
#    curl -s https://codecov.io/bash | CI_JOB_ID="${JOB_NAME}" CI_BUILD_ID="${BUILD_NUMBER}" bash /dev/stdin \
#      -K -Z -B ${PULL_BASE_REF} -C ${GIT_SHA} -P ${PULL_NUMBER} -t @/etc/codecov/broker.token
#else
#    echo "Not in bootstrap environment, skipping code coverage publishing"
#fi

echo "=== Publish docker images ==="
./bin/publish-docker-images.sh -t ${GIT_SHA} -h 'gcr.io/istio-testing'
