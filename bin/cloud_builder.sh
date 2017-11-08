#!/bin/bash
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
#
################################################################################

set -o errexit
set -o nounset
set -o pipefail
set -x

OUTPUT_PATH=""
# default is based on repo manifest that places istio at:
# go/src/istio.io/istio
# and proxy at:
# src/proxy
PROXY_PATH="../../../../src/proxy"
TAG_NAME="0.0.0"

function usage() {
  echo "$0
    -o        path to store build artifacts
    -p        path to proxy repo (relative to istio repo, defaults to ${PROXY_PATH} ) 
    -t <tag>  tag to use (optional, defaults to ${TAG_NAME} )"
  exit 1
}

while getopts o:p:t: arg ; do
  case "${arg}" in
    o) OUTPUT_PATH="${OPTARG}";;
    p) PROXY_PATH="${PROXY_PATH}";;
    t) TAG_NAME="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_PATH}" ]] && usage
[[ -z "${PROXY_PATH}" ]] && usage

# switch to the root of the istio repo
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd $ROOT

export GOPATH="$(cd "$ROOT/../../.." && pwd)"
echo gopath is $GOPATH

if [ ! -d "${PROXY_PATH}" ]; then
  echo "proxy dir not detected at ${PROXY_PATH}"
  usage
fi

pushd "${PROXY_PATH}"

# Use this file for Cloud Builder specific settings.
echo 'Setting bazel.rc'
cp tools/bazel.rc.cloudbuilder "${HOME}/.bazelrc"

####./script/push-debian.sh -c opt -v "${TAG_NAME}" -o "${OUTPUT_PATH}"
# TODO: run bazel in batch mode.  For now just shutdown bazel to save memory.
####bazel shutdown
popd

pushd security
# An empty hub skips the tag and push steps.  -h "" provokes unset var error msg.
####./bin/push-docker           -h " " -t "${TAG_NAME}" -b -o "${OUTPUT_PATH}"
####./bin/push-debian.sh -c opt -v "${TAG_NAME}" -o "${OUTPUT_PATH}"
popd

pushd mixer
####./bin/push-docker           -h " " -t "${TAG_NAME}" -b -o "${OUTPUT_PATH}"
popd

pushd pilot
## Cloud Builder checks out code in /workspace.
## We need to recreate the GOPATH directory structure
## for pilot to build correctly
#function prepare_gopath() {
#  [[ -z ${GOPATH:-} ]] && export GOPATH=/tmp/gopath
##  mkdir -p ${GOPATH}/src/istio.io/istio
##  [[ -d ${GOPATH}/src/istio.io/istio/pilot ]] || ln -s ${PWD} ${GOPATH}/src/istio.io/istio/pilot
#  # try symlink to istio rather than pilot so WORKPACE is visible
#  mkdir -p ${GOPATH}/src/istio.io
#  [[ -d ${GOPATH}/src/istio.io/istio ]] || ln -s ${PWD}/.. ${GOPATH}/src/istio.io/istio
#  cd ${GOPATH}/src/istio.io/istio/pilot
#  touch platform/kube/config
#}

## this is a bit convoluted to avoid unset var complaint on GOPTH
#if [ -z ${GOPATH:-} ]; then
#  prepare_gopath
#else
#  if [ ${PWD} != "${GOPATH}/src/istio.io/pilot" ]; then
#    prepare_gopath
#  fi
#fi

# Build istioctl binaries
# ./bin/init.sh
touch platform/kube/config
bazel build //...

./bin/upload-istioctl -r -o "${OUTPUT_PATH}"

./bin/push-docker -h " " -t "${TAG_NAME}" -b -o "${OUTPUT_PATH}"

# -v controls whether to do -release or not
./bin/push-debian.sh -c opt -v "${TAG_NAME}" -o "${OUTPUT_PATH}"
popd

# storing of artifacts is currently a separate cloud builder step
