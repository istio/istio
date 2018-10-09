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
ISTIOCTL_DOCKER_HUB=""

function usage() {
  echo "$0
    -b        opts out of building debian artifacts
    -c        opts out of building docker artifacts
    -h        docker hub to use for istioctl, deb and helm values.yaml
    -o        path to store build artifacts
    -t <tag>  tag to use"
  exit 1
}

while getopts bch:o:t: arg ; do
  case "${arg}" in
    b) BUILD_DEBIAN="false";;
    c) BUILD_DOCKER="false";;
    h) ISTIOCTL_DOCKER_HUB="${OPTARG}";;
    o) OUTPUT_PATH="${OPTARG}";;
    t) TAG_NAME="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_PATH}" ]] && usage
[[ -z "${TAG_NAME}" ]] && usage
[[ -z "${ISTIOCTL_DOCKER_HUB}" ]] && usage

# switch to the root of the istio repo
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GOPATH="$(cd "$ROOT/../../.." && pwd)"
export GOPATH
echo gopath is "$GOPATH"
ISTIO_OUT=$(make DEBUG=0 where-is-out)

export ISTIO_VERSION="${TAG_NAME}"

MAKE_TARGETS=(istio-archive)
if [ "${BUILD_DEBIAN}" == "true" ]; then
  MAKE_TARGETS+=(sidecar.deb)
fi

VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB=${ISTIOCTL_DOCKER_HUB} HUB=${ISTIOCTL_DOCKER_HUB} VERSION=$ISTIO_VERSION TAG=$ISTIO_VERSION make "${MAKE_TARGETS[@]}"
cp "${ISTIO_OUT}"/archive/istio-*z* "${OUTPUT_PATH}/"

if [ "${BUILD_DOCKER}" == "true" ]; then
  REL_DOCKER_HUB=docker.io/istio

  # we always save the docker tars and point them to docker.io/istio ($REL_DOCKER_HUB)
  # later scripts retag the tars with <hub>:$ISTIO_VERSION and push to <hub>:$ISTIO_VERSION 
  BUILD_DOCKER_TARGETS=(docker.save)
  VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB=${REL_DOCKER_HUB} HUB=${REL_DOCKER_HUB} VERSION=$ISTIO_VERSION TAG=$ISTIO_VERSION make "${BUILD_DOCKER_TARGETS[@]}"
  cp -r "${ISTIO_OUT}/docker" "${OUTPUT_PATH}/"

  SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
  # shellcheck source=release/docker_tag_push_lib.sh
  source "${SCRIPTPATH}/docker_tag_push_lib.sh"
  add_license_to_tar_images "${REL_DOCKER_HUB}" "${ISTIO_VERSION}" "${OUTPUT_PATH}"
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
