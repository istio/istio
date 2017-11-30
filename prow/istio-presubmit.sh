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

WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

# Check unset variables
set -u
# Print commands
set -x

# ensure Go version is same as bazel's Go version.
source "${ROOT}/bin/use_bazel_go.sh"
go version

# Exit immediately for non zero status
set -e

die () {
  echo "$@"
  exit 1
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

if [ "${CI:-}" == 'bootstrap' ]; then
  # Test harness will checkout code to directory $GOPATH/src/github.com/istio/istio
  # but we depend on being at path $GOPATH/src/istio.io/istio for imports
  mv ${GOPATH}/src/github.com/istio ${GOPATH}/src/istio.io
  ROOT=${GOPATH}/src/istio.io/istio
  cd ${GOPATH}/src/istio.io/istio

  # ln -sf ${GOPATH}/src/github.com/istio ${GOPATH}/src/istio.io
  # cd ${GOPATH}/src/istio.io/istio
  go get -u github.com/golang/dep/cmd/dep

  # Use the provided pull head sha, from prow.
  GIT_SHA="${PULL_PULL_SHA}"

  # check if rewrite history is present
  PR_BRANCH=$(git show-ref | grep refs/pr | awk '{print $2}')

  if [[ -z $PR_BRANCH ]];then
    echo "Could not get PR branch"
    git show-ref
    exit -1
  fi

  git ls-tree  $PR_BRANCH | grep .history_rewritten_20171102
  if [[ $? -ne 0 ]];then
    echo "This PR is from an out of date clone of istio.io/istio"
    echo "Create a fresh clone of istio.io/istio and re-submit the PR"
    exit -1
  fi

  # Use volume mount from pilot-presubmit job's pod spec.
  # FIXME pilot should not need this
  ln -sf "${HOME}/.kube/config" pilot/platform/kube/config
else
  # Use the current commit.
  GIT_SHA="$(git rev-parse --verify HEAD)"
fi
cd $ROOT

# ./bin/generate-protos.sh || die "Could not generate *.pb.go"
# if [[ -n $(git status --porcelain) ]]; then
#     git status
#     die "Repo has unstaged changes. Re-run ./bin/generate-protos.sh"
# fi

mkdir -p ~/envoy
cd ~/envoy
ISTIO_PROXY_BUCKET=$(sed 's/ = /=/' <<< $( awk '/ISTIO_PROXY_BUCKET =/' $ROOT/WORKSPACE))
PROXYVERSION=$(sed 's/[^"]*"\([^"]*\)".*/\1/' <<<  $ISTIO_PROXY_BUCKET)
PROXY=debug-$PROXYVERSION
wget -qO- https://storage.googleapis.com/istio-build/proxy/envoy-$PROXY.tar.gz | tar xvz
ln -sf ~/envoy/usr/local/bin/envoy $ROOT/pilot/proxy/envoy/envoy
cd $ROOT

# go test execution
# time dep ensure -v

# echo FIXME remove mixer tools exclusion after tests can be run without bazel
# time go test $(go list ./mixer/... | grep -v /tools/codegen)
# time go test ./pilot/...
# time go test ./security/...
# time go test ./broker/...
# rm -rf vendor/

if [ "${CI:-}" == 'bootstrap' ]; then
  # Test harness will checkout code to directory $GOPATH/src/github.com/istio/istio
  # but we depend on being at path $GOPATH/src/istio.io/istio for imports
  mv ${GOPATH}/src/istio.io/istio ${GOPATH}/src/github.com
  ln -sf ${GOPATH}/src/github.com/istio ${GOPATH}/src/istio.io
  ROOT=${GOPATH}/src/istio.io/istio
  cd ${GOPATH}/src/istio.io/istio
fi

# Build
${ROOT}/bin/init.sh

run_or_die_on_change ./bin/fmt.sh

# bazel test execution
echo 'Running Unit Tests'
time bazel test --test_output=all //...

# run linters in advisory mode
SKIP_INIT=1 ${ROOT}/bin/linters.sh

diff=`git diff`
if [[ -n "$diff" ]]; then
  echo "Some uncommitted changes are found. Maybe miss committing some generated files? Here's the diff"
  echo $diff
  # Do not fail for the changes for now; presubmit bot may share the bazel-genfiles, and that will cause
  # unrelated failure here randomly. See https://github.com/istio/istio/issues/1689 for the details.
  # TODO: fix the problem and fail here again.
  # exit -1
fi

HUB="gcr.io/istio-testing"
TAG="${GIT_SHA}"
# upload images
time make push HUB="${HUB}" TAG="${TAG}"
