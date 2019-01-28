#!/bin/bash

# Copyright 2018 Istio Authors
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

set -e
set -u
set -o pipefail

SCRIPTPATH="$(cd "$(dirname "$0")" ; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"

REPORT_PATH=${GOPATH}/out/codecov/pr
BASELINE_PATH=${GOPATH}/out/codecov/baseline
CODECOV_SKIP=${GOPATH}/out/codecov/codecov.skip
THRESHOLD_FILE=${GOPATH}/out/codecov/codecov.threshold
mkdir -p "${GOPATH}"/out/codecov
mkdir -p "${GOPATH}"/out/tests

# Use the codecov.skip from the PR across two test runs to make sure we skip a
# consistent list of packages.
cp "${ROOTDIR}"/codecov.skip "${CODECOV_SKIP}"
cp "${ROOTDIR}"/codecov.threshold "${THRESHOLD_FILE}"

# First run codecov from current workspace (PR)
OUT_DIR="${REPORT_PATH}" MAXPROCS="${MAXPROCS:-}" CODECOV_SKIP="${CODECOV_SKIP:-}" ./bin/codecov.sh

if [[ -n "${CIRCLE_PR_NUMBER:-}" ]]; then
  TMP_GITHUB_TOKEN=$(mktemp /tmp/XXXXX.github)
  openssl version
  openssl aes-256-cbc -d -in .circleci/accounts/istio-github.enc -out "${TMP_GITHUB_TOKEN}" -k "${GCS_BUCKET_TOKEN}" -md sha256

  # Backup codecov.sh since the base SHA may not have this copy.
  TMP_CODECOV_SH=$(mktemp /tmp/XXXXX.codecov)
  cp ./bin/codecov.sh "${TMP_CODECOV_SH}"

  go get -u istio.io/test-infra/toolbox/githubctl
  BASE_SHA=$("${GOPATH}"/bin/githubctl --token_file="${TMP_GITHUB_TOKEN}" --op=getBaseSHA --repo=istio --pr_num="${CIRCLE_PR_NUMBER}")
  git reset HEAD --hard
  git clean -f -d
  git checkout "${BASE_SHA}"

  cp "${TMP_CODECOV_SH}" ./bin/codecov.sh

  # Run test at the base SHA
  OUT_DIR="${BASELINE_PATH}" MAXPROCS="${MAXPROCS:-}" CODECOV_SKIP="${CODECOV_SKIP:-}" ./bin/codecov.sh

  # Get back to the PR head
  git reset HEAD --hard
  git clean -f -d
  git checkout "${CIRCLE_SHA1}"

  # Test that coverage is not dropped
  go test -v istio.io/istio/tests/codecov/... \
    --report_file="${REPORT_PATH}/coverage.html" \
    --baseline_file="${BASELINE_PATH}/coverage.html" \
    --threshold_files="${THRESHOLD_FILE},${CODECOV_SKIP}" \
    | tee "${GOPATH}"/out/codecov/out.log \
    | tee >(go-junit-report > "${GOPATH}"/out/tests/junit.xml)
else
  # Upload to codecov.io in post submit only for visualization
  bash <(curl -s https://codecov.io/bash) -f /go/out/codecov/pr/coverage.cov
fi

