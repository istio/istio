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

DEFAULT_GCS_PREFIX="istio-testing/builds"
DEFAULT_GCR_PREFIX="istio-testing"

GCS_PREFIX=""
GCR_PREFIX=""

VER_STRING="0.0.0"
OUTPUT_PATH=""
BUILD_ID=""
PUSH_DOCKER="true"

function usage() {
  echo "$0
    -i <id>   build ID from cloud builder                       (optional, currently unused)
    -n        disable pushing docker images to GCR              (optional)
    -o <path> src path where build output/artifacts were stored (required)
    -p <name> GCS bucket & prefix path where to store build     (optional, defaults to ${DEFAULT_GCS_PREFIX} )
    -q <name> GCR bucket & prefix path where to store build     (optional, defaults to ${DEFAULT_GCR_PREFIX} )
    -v <ver>  version string for tag & defaulted storage paths  (optional, defaults to ${VER_STRING} )"
  exit 1
}

while getopts abi:no:p:q:v: arg ; do
  case "${arg}" in
    i) BUILD_ID="${OPTARG}";;
    n) PUSH_DOCKER="false";;
    o) OUTPUT_PATH="${OPTARG}";;
    p) GCS_PREFIX="${OPTARG}";;
    q) GCR_PREFIX="${OPTARG}";;
    v) VER_STRING="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_PATH}" ]] && usage
[[ -z "${VER_STRING}" ]] && usage

# remove any trailing / from GCR_PREFIX since docker doesn't like to see //
# do the same for GCS for consistency

GCR_PREFIX=${GCR_PREFIX%/}
GCS_PREFIX=${GCS_PREFIX%/}
  
if [[ -z "${GCS_PREFIX}"  ]]; then
  GCS_PREFIX="${DEFAULT_GCS_PREFIX}"
fi

if [[ -z "${GCR_PREFIX}"  ]]; then
  GCR_PREFIX="${DEFAULT_GCR_PREFIX}"
fi

GCS_PATH="gs://${GCS_PREFIX}"
GCR_PATH="gcr.io/${GCR_PREFIX}"

gsutil -m cp -r "${OUTPUT_PATH}/*" "${GCS_PATH}/"

if [[ "${PUSH_DOCKER}" == "true" ]]; then
  for TAR_PATH in ${OUTPUT_PATH}/docker/*.tar
  do
    TAR_NAME=$(basename "$TAR_PATH")
    IMAGE_NAME="${TAR_NAME%.*}"
    
    # if no docker/ directory or directory has no tar files
    if [[ "${IMAGE_NAME}" == "*" ]]; then
      break
    fi
    docker import "${TAR_PATH}" "${IMAGE_NAME}:${VER_STRING}"
    docker tag "${IMAGE_NAME}:${VER_STRING}" "${GCR_PATH}/${IMAGE_NAME}:${VER_STRING}"
    gcloud docker -- push "${GCR_PATH}/${IMAGE_NAME}:${VER_STRING}"
  done
fi
