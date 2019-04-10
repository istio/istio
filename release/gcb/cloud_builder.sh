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
# create_release_archives.sh (via make istio-archive) to add tar files to the directory
# (based solely on the contents of that directory), and then
# uses store_artifacts.sh to store the build on GCS and docker hub
# in addition it saves the source code

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/docker_tag_push_lib.sh
source "${SCRIPTPATH}/docker_tag_push_lib.sh"

# directory that has the artifacts, hardcoded since the volume name in cloud_builder.json
# need to be the same also there is no value in making this configurable
OUTPUT_PATH="/output"

ROOT="$PWD"
GOPATH="$(cd "$ROOT/../../.." && pwd)"
export GOPATH
echo gopath is "$GOPATH"
# this is needed for istioctl and other parts of build to get the version info
export ISTIO_VERSION="${CB_VERSION}"

ISTIO_OUT=$(make DEBUG=0 where-is-out)

MAKE_TARGETS=(istio-archive)
MAKE_TARGETS+=(sidecar.deb)

VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB="${CB_ISTIOCTL_DOCKER_HUB}" HUB="${CB_ISTIOCTL_DOCKER_HUB}" VERSION="${CB_VERSION}" TAG="${CB_VERSION}" make "${MAKE_TARGETS[@]}"
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

# preserve the source from the root of the code
pushd "${ROOT}/../../../.."
pwd
# tar the source code
tar -czf "${OUTPUT_PATH}/source.tar.gz" go src --exclude go/out --exclude go/bin
popd

cp -r "${ISTIO_OUT}/docker" "${OUTPUT_PATH}/"

go run tools/license/get_dep_licenses.go --branch "${CB_BRANCH}" > LICENSES.txt

# Add extra artifacts for legal compliance. The caller of this script can
# optionally set their EXTRA_ARTIFACTS environment variable to an arbitrarily
# long list of space-delimited filepaths --- and each artifact would get
# injected into the Docker image.
add_extra_artifacts_to_tar_images \
  "${REL_DOCKER_HUB}" \
  "${CB_VERSION}" \
  "${OUTPUT_PATH}" \
  "${EXTRA_ARTIFACTS:-$PWD/LICENSES.txt}"

# log where git thinks the build might be dirty
git status

cp "/workspace/manifest.txt" "${OUTPUT_PATH}"

#Handle CNI artifacts.
pushd ../cni
CNI_OUT=$(make DEBUG=0 where-is-out)
# CNI version strategy is to have CNI run lock step with Istio i.e. CB_VERSION
VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB="${CB_ISTIOCTL_DOCKER_HUB}" HUB="${CB_ISTIOCTL_DOCKER_HUB}" VERSION="${ISTIO_VERSION}" TAG="${ISTIO_VERSION}" make build

if [ -d "${CNI_OUT}/docker" ]; then
    rm -r "${CNI_OUT}/docker"
fi

VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB=${REL_DOCKER_HUB} HUB=${REL_DOCKER_HUB} VERSION="${ISTIO_VERSION}" TAG="${ISTIO_VERSION}" make docker.save

mkdir "/cni_tmp"

cp -r "${CNI_OUT}/docker" "/cni_tmp"

go run ../istio/tools/license/get_dep_licenses.go --branch "${CB_BRANCH}" > LICENSES.txt
add_extra_artifacts_to_tar_images \
  "${REL_DOCKER_HUB}" \
  "${CB_VERSION}" \
  "/cni_tmp" \
  "${EXTRA_ARTIFACTS_CNI:-$PWD/LICENSES.txt}"
cp -r "/cni_tmp/docker" "${OUTPUT_PATH}/"

# log where git thinks the build might be dirty
git status

popd
