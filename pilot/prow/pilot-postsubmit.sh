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


########################################
# Postsubmit script triggered by Prow. #
########################################

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

if [ "${CI:-}" == 'bootstrap' ]; then
    # Test harness will checkout code to directory $GOPATH/src/github.com/istio/istio
    # but we depend on being at path $GOPATH/src/istio.io/istio for imports.
    ln -sf ${GOPATH}/src/github.com/istio ${GOPATH}/src/istio.io
    cd ${GOPATH}/src/istio.io/pilot

    # Use the provided base sha, from prow.
    GIT_SHA="${PULL_BASE_SHA}"

    # Use volume mount from pilot-presubmit job's pod spec.
    ln -sf "${HOME}/.kube/config" platform/kube/config
else
    # Use the current commit.
    GIT_SHA="$(git rev-parse --verify HEAD)"
fi

echo '=== Prerequisites ==='
./bin/install-prereqs.sh

echo '=== Go Build ==='
./bin/init.sh

echo '=== Bazel Tests ==='
bazel test //...

echo  '=== Code Coverage ==='
./bin/codecov.sh | tee codecov.report
if [ "${CI:-}" == 'bootstrap' ]; then
    BUILD_ID="PROW-${BUILD_NUMBER}" JOB_NAME='pilot/postsubmit'

    curl -s https://codecov.io/bash \
      | CI_JOB_ID="${JOB_NAME}" CI_BUILD_ID="${BUILD_NUMBER}" bash /dev/stdin \
        -K -B "${PULL_BASE_REF}" -C "${PULL_BASE_SHA}" -t @/etc/codecov/pilot.token
else
    echo 'Not in bootstrap environment, skipping code coverage publishing'
fi
#./bin/toolbox/pkg_coverage.sh

echo '=== Build istioctl ==='
./bin/upload-istioctl -r -p "gs://istio-artifacts/pilot/${GIT_SHA}/artifacts/istioctl"

echo "=== Pushing Debian Packages ==="
bin/push-debian.sh -c opt -p "gs://istio-artifacts/pilot/${GIT_SHA}/artifacts/debs"

echo '=== Running e2e Tests ==='
bin/e2e.sh -logs=false -tag "${GIT_SHA}"

if [ "${CI:-}" == 'bootstrap' ]; then
    echo "=== Building githubctl ==="
    bazel build @com_github_istio_test_infra//toolbox/githubctl:githubctl

    echo "=== Fast-forwarding stable branch to new SHA ==="
    bazel-bin/external/com_github_istio_test_infra/toolbox/githubctl/githubctl \
        --token_file=/etc/github/oauth \
        --op=fastForward \
        --repo=pilot \
        --base_branch=stable \
        --ref_sha=$GIT_SHA
else
    echo "Not in bootstrap environment, skipping stable branch promotion"
fi
