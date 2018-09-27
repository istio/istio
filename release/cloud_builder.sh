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
TAG_NAME=""
BUILD_DEBIAN="true"
BUILD_DOCKER="true"
REL_DOCKER_HUB=docker.io/istio
TEST_DOCKER_HUB=""
TEST_GCS_PATH=""

function usage() {
  echo "$0
    -b        opts out of building debian artifacts
    -c        opts out of building docker artifacts
    -h        docker hub to use for testing (optional)
    -o        path to store build artifacts
    -p        GCS bucket & prefix path where build will be stored for testing (optional)
    -q        path on gcr hub to use for testing (optional, alt to -h)
    -t <tag>  tag to use"
  exit 1
}

while getopts bch:o:p:q:t: arg ; do
  case "${arg}" in
    b) BUILD_DEBIAN="false";;
    c) BUILD_DOCKER="false";;
    h) TEST_DOCKER_HUB="${OPTARG}";;
    p) TEST_GCS_PATH="${OPTARG}";;
    q) TEST_DOCKER_HUB="gcr.io/${OPTARG}";;
    o) OUTPUT_PATH="${OPTARG}";;
    t) TAG_NAME="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_PATH}" ]] && usage
[[ -z "${TAG_NAME}" ]] && usage

DEFAULT_GCS_PATH="https://storage.googleapis.com/istio-release/releases/${TAG_NAME}"
TEST_PATH=${TEST_GCS_PATH:-$DEFAULT_GCS_PATH}

# switch to the root of the istio repo
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd $ROOT

export GOPATH="$(cd "$ROOT/../../.." && pwd)"
echo gopath is $GOPATH
ISTIO_OUT=$(make DEBUG=0 where-is-out)

export ISTIO_VERSION="${TAG_NAME}"

MAKE_TARGETS=istio-archive
if [ "${BUILD_DEBIAN}" == "true" ]; then
  MAKE_TARGETS="sidecar.deb ${MAKE_TARGETS}"
fi
if [ "${BUILD_DOCKER}" == "true" ]; then
  MAKE_TARGETS="docker.save ${MAKE_TARGETS}"
fi

if [[ -n "${TEST_DOCKER_HUB}" ]]; then
  VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB=${TEST_DOCKER_HUB} HUB=${TEST_DOCKER_HUB} VERSION=$ISTIO_VERSION TAG=$ISTIO_VERSION ISTIO_GCS=$TEST_PATH make istio-archive
  cp "${ISTIO_OUT}"/archive/istio-*z* "${OUTPUT_PATH}/"
  mkdir -p "${OUTPUT_PATH}/gcr.io"
  cp "${ISTIO_OUT}"/archive/istio-*z* "${OUTPUT_PATH}/gcr.io/"
fi

VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB=${REL_DOCKER_HUB} HUB=${REL_DOCKER_HUB} VERSION=$ISTIO_VERSION TAG=$ISTIO_VERSION make ${MAKE_TARGETS}
cp "${ISTIO_OUT}"/archive/istio-*z* "${OUTPUT_PATH}/"
mkdir -p "${OUTPUT_PATH}/docker.io"
cp "${ISTIO_OUT}"/archive/istio-*z* "${OUTPUT_PATH}/docker.io/"

if [[ -n "${TEST_DOCKER_HUB}" ]]; then
# this copy is being done here instead of above because we
# are conservative, if new artifacts are created we don't
# inadvertently clobber them
  cp "${OUTPUT_PATH}"/gcr.io/istio-*z* "${OUTPUT_PATH}/"
fi

if [ "${BUILD_DOCKER}" == "true" ]; then
  cp -r ${ISTIO_OUT}/docker ${OUTPUT_PATH}/
fi

if [ "${BUILD_DEBIAN}" == "true" ]; then
  mkdir -p "${OUTPUT_PATH}/deb"
  cp        "${ISTIO_OUT}/istio-sidecar.deb"   "${OUTPUT_PATH}/deb/"
  pushd "${OUTPUT_PATH}/deb"
    sha256sum "istio-sidecar.deb" > "istio-sidecar.deb.sha256"
  popd
fi

# log where git thinks the build might be dirty
git status
