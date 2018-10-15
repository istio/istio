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
# uses store_artifacts.sh to store the build on GCS and docker hub
# in addition it saves the source code

source "/workspace/gcb_env.sh"

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/docker_tag_push_lib.sh
source "${SCRIPTPATH}/docker_tag_push_lib.sh"

function usage() {
  echo "$0
        uses CB_ISTIOCTL_DOCKER_HUB CB_VERSION"
  exit 1
}

[[ -z "${CB_VERSION}" ]] && usage
[[ -z "${CB_ISTIOCTL_DOCKER_HUB}" ]] && usage

OUTPUT_PATH="/output"

# switch to the root of the istio repo
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GOPATH="$(cd "$ROOT/../../.." && pwd)"
export GOPATH
echo gopath is "$GOPATH"
ISTIO_OUT=$(make DEBUG=0 where-is-out)

MAKE_TARGETS=(istio-archive)
MAKE_TARGETS+=(sidecar.deb)

VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB="${CB_ISTIOCTL_DOCKER_HUB}" HUB="${CB_STIOCTL_DOCKER_HUB}" VERSION="${CB_VERSION}" TAG="${CB_VERSION}" make "${MAKE_TARGETS[@]}"
mkdir -p "${OUTPUT_PATH}/deb"
sha256sum "${ISTIO_OUT}/istio-sidecar.deb" > "${OUTPUT_PATH}/deb/istio-sidecar.deb.sha256"
cp        "${ISTIO_OUT}/istio-sidecar.deb"   "${OUTPUT_PATH}/deb/"
cp        "${ISTIO_OUT}"/archive/istio-*z*   "${OUTPUT_PATH}/"


# build docker tar images
REL_DOCKER_HUB=docker.io/istio

# we always save the docker tars and point them to docker.io/istio ($REL_DOCKER_HUB)
# later scripts retag the tars with <hub>:$CB_VERSION and push to <hub>:$CB_VERSION 
BUILD_DOCKER_TARGETS=(docker.save)

VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB=${REL_DOCKER_HUB} HUB=${REL_DOCKER_HUB} VERSION="${CB_VERSION}" TAG="${CB_VERSION}" make "${BUILD_DOCKER_TARGETS[@]}"

cp -r "${ISTIO_OUT}/docker" "${OUTPUT_PATH}/"

add_license_to_tar_images "${REL_DOCKER_HUB}" "${CB_VERSION}" "${OUTPUT_PATH}"

# log where git thinks the build might be dirty
git status

# preserve the source from the root of the code
pushd "${ROOT}/../../../.."
pwd
# tar the source code
tar -czf "${OUTPUT_PATH}/source.tar.gz" go src --exclude go/out --exclude go/bin
popd

cp "/workspace/manifest.txt" "${OUTPUT_PATH}"
