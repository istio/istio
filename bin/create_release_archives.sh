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

BASE_DIR="$(mktemp -d /tmp/istio.version.XXXX)"
OUTPUT_PATH=""
VER_STRING=""

function usage() {
  echo "$0
    -d <path> path to use for temp directory                  (optional, randomized default is ${BASE_DIR} )
    -o <path> path where build output/artifacts are stored    (required)
    -v <ver>  version info to include in filename (e.g., 1.0) (required)"
  exit 1
}

function error_exit() {
  # ${BASH_SOURCE[1]} is the file name of the caller.
  echo "${BASH_SOURCE[1]}: line ${BASH_LINENO[0]}: ${1:-Unknown Error.} (exit ${2:-1})" 1>&2
  exit ${2:-1}
}

while getopts d:o:v: arg ; do
  case "${arg}" in
    d) BASE_DIR="${OPTARG}";;
    o) OUTPUT_PATH="${OPTARG}";;
    v) VER_STRING="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${BASE_DIR}"  ]] && usage
[[ -z "${OUTPUT_PATH}"  ]] && usage
[[ -z "${VER_STRING}"   ]] && usage

COMMON_FILES_DIR="${BASE_DIR}/istio/istio-${VER_STRING}"
BIN_DIR="${COMMON_FILES_DIR}/bin"
mkdir -p "${BIN_DIR}"

# On mac, brew install gnu-tar gnu-cp
# and set CP=gcp TAR=gtar

if [[ -z "${CP}" ]] ; then
  CP=cp
fi
if [[ -z "${TAR}" ]] ; then
  TAR=tar
fi

function create_linux_archive() {
  local istioctl_path="${BIN_DIR}/istioctl"

  ${CP} "${OUTPUT_PATH}/istioctl-linux" "${istioctl_path}"
  chmod 755 "${istioctl_path}"

  ${TAR} --owner releng --group releng -czvf \
    "${OUTPUT_PATH}/istio-${VER_STRING}-linux.tar.gz" "istio-${VER_STRING}" \
    || error_exit 'Could not create linux archive'
  rm "${istioctl_path}"
}

function create_osx_archive() {
  local istioctl_path="${BIN_DIR}/istioctl"

  ${CP} "${OUTPUT_PATH}/istioctl-osx" "${istioctl_path}"
  chmod 755 "${istioctl_path}"

  ${TAR} --owner releng --group releng -czvf \
    "${OUTPUT_PATH}/istio-${VER_STRING}-osx.tar.gz" "istio-${VER_STRING}" \
    || error_exit 'Could not create osx archive'
  rm "${istioctl_path}"
}

function create_windows_archive() {
  local istioctl_path="${BIN_DIR}/istioctl.exe"

  ${CP} "${OUTPUT_PATH}/istioctl-win.exe" "${istioctl_path}"
  
  zip -r "${OUTPUT_PATH}/istio_${VER_STRING}_win.zip" "istio-${VER_STRING}" \
    || error_exit 'Could not create windows archive'
  rm "${istioctl_path}"
}

pushd "${OUTPUT_PATH}"
${CP} istio.VERSION LICENSE README.md CONTRIBUTING.md "${COMMON_FILES_DIR}"/
find samples install -type f \( -name "*.yaml" -o -name "cleanup*" -o -name "*.md" \) \
  -exec ${CP} --parents {} "${COMMON_FILES_DIR}" \;
find install/tools -type f -exec ${CP} --parents {} "${COMMON_FILES_DIR}" \;
popd

# Changing dir such that tar and zip files are
# created with right hiereachy
pushd "${COMMON_FILES_DIR}/.."
create_linux_archive
create_osx_archive
create_windows_archive
popd
