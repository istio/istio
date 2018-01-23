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

# This file is primarily used by cloud builder to make
# an end-to-end built of istio.  It runs this script to place the
# build artifacts in a specified output directory, then runs
# create_release_archives.sh to add tar files to the directory
# (based solely on the contents of that directory), and then
# uses store_artifacts.sh to store the build on GCR/GCS.

OUTPUT_PATH=""
TAG_NAME="0.0.0"
BUILD_DEBIAN="true"
BUILD_DOCKER="true"
REL_DOCKER_HUB=docker.io/istio
TEST_DOCKER_HUB=""

function usage() {
  echo "$0
    -b        opts out of building debian artifacts
    -c        opts out of building docker artifacts
    -h        docker hub to use for testing (optional)
    -o        path to store build artifacts
    -q        path on gcr hub to use for testing (optional, alt to -h)
    -t <tag>  tag to use (optional, defaults to ${TAG_NAME} )"
  exit 1
}

while getopts bch:o:q:t: arg ; do
  case "${arg}" in
    b) BUILD_DEBIAN="false";;
    c) BUILD_DOCKER="false";;
    h) TEST_DOCKER_HUB="${OPTARG}";;
    q) TEST_DOCKER_HUB="gcr.io/${OPTARG}";;
    o) OUTPUT_PATH="${OPTARG}";;
    t) TAG_NAME="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_PATH}" ]] && usage

# switch to the root of the istio repo
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd $ROOT

export GOPATH="$(cd "$ROOT/../../.." && pwd)"
echo gopath is $GOPATH

export ISTIO_VERSION="${TAG_NAME}"

apt-get -qqy install ruby ruby-dev rubygems build-essential
gem install --no-ri --no-rdoc fpm

VERBOSE=1 make setup

VERBOSE=1 make init

# pull in outside dependencies
VERBOSE=1 make depend

if [ "${BUILD_DEBIAN}" == "true" ]; then
  # OUT="${OUTPUT_PATH}/deb" is ignored so we'll have to do the copy
  # and hope that the name of the file doesn't change.
  VERBOSE=1 VERSION=$ISTIO_VERSION ISTIO_DOCKER_HUB=$REL_DOCKER_HUB TAG=$ISTIO_VERSION make sidecar.deb
  mkdir -p ${OUTPUT_PATH}/deb
  cp ${GOPATH}/out/istio-sidecar.deb ${OUTPUT_PATH}/deb
fi

pushd pilot
mkdir -p "${OUTPUT_PATH}/istioctl"
# make istioctl just outputs to pilot/cmd/istioctl
ISTIO_DOCKER_HUB=${REL_DOCKER_HUB}  ./bin/upload-istioctl -r -o "${OUTPUT_PATH}/istioctl"
if [[ -n "${TEST_DOCKER_HUB}" ]]; then
   mkdir -p "${OUTPUT_PATH}/istioctl-stage"
   ISTIO_DOCKER_HUB=${TEST_DOCKER_HUB} ./bin/upload-istioctl -r -o "${OUTPUT_PATH}/istioctl-stage"
fi
popd

if [ "${BUILD_DOCKER}" == "true" ]; then
  VERBOSE=1 VERSION=$ISTIO_VERSION ISTIO_DOCKER_HUB=$REL_DOCKER_HUB TAG=$ISTIO_VERSION make docker.save
  cp -r ${GOPATH}/out/docker ${OUTPUT_PATH}
fi

# log where git thinks the build might be dirty
git status
