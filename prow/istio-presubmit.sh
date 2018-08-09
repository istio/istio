#!/bin/bash

# Copyright 2016 Istio Authors
#
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

# Presubmit script triggered by Prow.
# - push docker images to grc.io for the integration tests.

# Separate (and parallel) jobs are doing lint, coverage, etc.

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# No unset vars, print commands as they're executed, and exit on any non-zero
# return code
set -u
set -x
set -e

source "${ROOT}/prow/lib.sh"
setup_and_export_git_sha

echo 'Build'
(cd "${ROOT}"; make build)

if [[ -n $(git diff) ]]; then
  echo "Uncommitted changes found:"
  git diff
fi

# Upload images - needed by the subsequent tests
time ISTIO_DOCKER_HUB="gcr.io/istio-testing" make push HUB="gcr.io/istio-testing" TAG="${GIT_SHA}"

