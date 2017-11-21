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
set -o pipefail
set -x

# This script primarily exists for Cloud Builder.  This script
# reads artifacts from a specified directory, generates tar files
# based on those artifacts, and then stores the tar files
# back to the directory.

OUTPUT_PATH=""
VER_STRING=""
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GCR_TEST_PATH=""
GCS_TEST_PATH=""

function usage() {
  echo "$0
    -o <path> path where build output/artifacts are stored        (required)
    -p <name> GCS bucket & prefix path where build will be stored (optional)
    -q <name> GCR bucket & prefix path where build will be stored (optional [required if -p used] )
    -v <ver>  version info to include in filename (e.g., 1.0)     (required)"
  exit 1
}

function error_exit() {
  # ${BASH_SOURCE[1]} is the file name of the caller.
  echo "${BASH_SOURCE[1]}: line ${BASH_LINENO[0]}: ${1:-Unknown Error.} (exit ${2:-1})" 1>&2
  exit ${2:-1}
}

while getopts o:p:q:v: arg ; do
  case "${arg}" in
    o) OUTPUT_PATH="${OPTARG}";;
    p) GCS_TEST_PATH="${OPTARG}";;
    q) GCR_TEST_PATH="${OPTARG}";;
    v) VER_STRING="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_PATH}"  ]] && usage
[[ -z "${VER_STRING}"   ]] && usage

function copy_and_archive() {
  # can't seem to set/override PROXY_TAG (sha), FORTIO_HUB ("docker.io/istio"), FORTIO_TAG ("0.3.1")
  ${ROOT}/install/updateVersion.sh -c "${DOCKER_HUB_TAG}" -A "${DEBIAN_URL}"   -x "${DOCKER_HUB_TAG}" \
                                   -p "${DOCKER_HUB_TAG}" -i "${ISTIOCTL_URL}" -P "${DEBIAN_URL}" \
                                   -r "${VER_STRING}" -E "${DEBIAN_URL}"
  # save -d "${OUTPUT_PATH}" for later
  
  pushd ${ROOT}
  cp istio.VERSION LICENSE README.md "${OUTPUT_PATH}/"
  find samples install -type f \( -name "*.yaml" -o -name "cleanup*" -o -name "*.md" \) \
    -exec cp --parents {} "${OUTPUT_PATH}" \;
  find install/tools -type f -exec cp --parents {} "${OUTPUT_PATH}" \;
  popd

  ${ROOT}/release/create_release_archives.sh -v "${VER_STRING}" -o "${OUTPUT_PATH}"
  return 0
}

# generate a test set of tars for images on GCR
if [[ -n "${GCR_TEST_PATH}" && -n "${GCS_TEST_PATH}" ]]; then

  # remove any trailing / since docker doesn't like to see //
  # do the same for GCS for consistency

  GCR_TEST_PATH=${GCR_TEST_PATH%/}
  GCS_TEST_PATH=${GCS_TEST_PATH%/}

  DOCKER_HUB_TAG="gcr.io/${GCR_TEST_PATH},${VER_STRING}"
  COMMON_URL="https://storage.googleapis.com/${GCS_TEST_PATH}"
  ISTIOCTL_URL="${COMMON_URL}/istioctl"
  DEBIAN_URL="${COMMON_URL}/deb"
  copy_and_archive

  # These files are only used for testing, so use a name to help make this clear
  for TAR_FILE in ${OUTPUT_PATH}/istio?${VER_STRING}*; do
    mv "$TAR_FILE" $(dirname "$TAR_FILE")/TESTONLY-$(basename "$TAR_FILE")
  done
fi

# generate the release set of tars
DOCKER_HUB_TAG="docker.io/istio,${VER_STRING}"
COMMON_URL="https://storage.googleapis.com/istio-release/releases/${VER_STRING}"
ISTIOCTL_URL="${COMMON_URL}/istioctl"
DEBIAN_URL="${COMMON_URL}/deb"
copy_and_archive
