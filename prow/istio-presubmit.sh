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

#######################################
# Presubmit script triggered by Prow. #
#######################################

WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

# No unset vars, print commands as they're executed, and exit on any non-zero
# return code
set -u
set -x
set -e

die () {
  echo "$@"
  exit -1
}

run_or_die_on_change() {
  local script=$1
  $script || die "Could not run ${script}"
  # "generated_files" can be modified by other presubmit runs, since
  # build caches are shared among them. For now, it should be excluded for
  # the observed changes.
  # TODO(https://github.com/istio/istio/issues/1689): fix this.
  if [[ -n $(git status --porcelain | grep -v generated_files) ]]; then
    git status
    die "Repo has unstaged changes. Re-run ${script}"
  fi
}

fetch_envoy() {
    local DIR=~/envoy
    mkdir -p ${DIR}
    pushd ${DIR}
    # In WORKSPACE, the string looks like:
    #       ISTIO_PROXY_BUCKET = "76ed00adea006e25878f20daa58c456848243999"
    # we pull it out, grab the SHA, and trim the quotes
    local PROXY_SHA=$(grep "ISTIO_PROXY_BUCKET =" ${ROOT}/WORKSPACE | cut -d' ' -f3 | tr -d '"')
    wget -qO- https://storage.googleapis.com/istio-build/proxy/envoy-debug-${PROXY_SHA}.tar.gz | tar xvz
    ln -sf ${DIR}/usr/local/bin/envoy ${ROOT}/pilot/proxy/envoy/envoy
    popd
}

# ensure our bazel and go env vars are set up correctly
source "${ROOT}/bin/use_bazel_go.sh"

if [ "${CI:-}" == 'bootstrap' ]; then
  # Test harness will checkout code to directory $GOPATH/src/github.com/istio/istio
  # but we depend on being at path $GOPATH/src/istio.io/istio for imports
  mv ${GOPATH}/src/github.com/istio ${GOPATH}/src/istio.io
  ROOT=${GOPATH}/src/istio.io/istio
  cd ${GOPATH}/src/istio.io/istio

  # Use the provided pull head sha, from prow.
  GIT_SHA="${PULL_PULL_SHA}"

  # check if rewrite history is present
  PR_BRANCH=$(git show-ref | grep refs/pr | awk '{print $2}')
  if [[ -z $PR_BRANCH ]];then
    echo "Could not get PR branch"
    die $(git show-ref)
  fi

  git ls-tree  $PR_BRANCH | grep .history_rewritten_20171102
  if [[ $? -ne 0 ]];then
    echo "This PR is from an out of date clone of istio.io/istio"
    die "Create a fresh clone of istio.io/istio and re-submit the PR"
  fi

  # Use volume mount from pilot-presubmit job's pod spec.
  # FIXME pilot should not need this
  ln -sf "${HOME}/.kube/config" pilot/platform/kube/config
else
  # Use the current commit.
  GIT_SHA="$(git rev-parse --verify HEAD)"
fi

echo 'Pulling down pre-built Envoy'
fetch_envoy

echo 'Building everything'
${ROOT}/bin/init.sh

# TODO(https://github.com/istio/istio/pull/1930): Uncomment
# run_or_die_on_change ./bin/fmt.sh

echo 'Running Unit Tests'
time bazel test --test_output=all //...

echo 'Running linters'
SKIP_INIT=1 ${ROOT}/bin/linters.sh || die "Error: linters.sh failed"

if [[ -n $(git diff) ]]; then
  echo "Uncommitted changes found:"
  git diff
fi

# upload images
time make push HUB="gcr.io/istio-testing" TAG="${GIT_SHA}"
