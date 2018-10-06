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

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# This script takes files from a specified directory and uploads
# then to GCR & GCS.  Only tar files in docker/ are uploaded to GCR.

DEFAULT_GCS_PREFIX="istio-testing/builds"

GCS_PREFIX=""

VER_STRING=""
OUTPUT_PATH=""
PUSH_DOCKER="true"
TEST_DOCKER_HUB=""

function usage() {
  echo "$0
    -c <name> Branch of the build                               (required)
    -h <hub>  docker hub to use (optional defaults to gcr.io/istio-release)
    -i <id>   build ID from cloud builder                       (optional, currently unused)
    -n        disable pushing docker images to GCR              (optional)
    -o <path> src path where build output/artifacts were stored (required)
    -p <name> GCS bucket & prefix path where to store build     (optional, defaults to ${DEFAULT_GCS_PREFIX} )
    -v <ver>  version string for tag & defaulted storage paths"
  exit 1
}

while getopts c:h:no:p:q:v: arg ; do
  case "${arg}" in
    c) BRANCH="${OPTARG}";;
    h) TEST_DOCKER_HUB="${OPTARG}";;
    n) PUSH_DOCKER="false";;
    o) OUTPUT_PATH="${OPTARG}";;
    p) GCS_PREFIX="${OPTARG}";;
    v) VER_STRING="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_PATH}" ]] && usage
[[ -z "${VER_STRING}"  ]] && usage
[[ -z "${BRANCH}"      ]] && usage

# remove any trailing / for GCS
GCS_PREFIX=${GCS_PREFIX%/}
GCS_PREFIX=${GCS_PREFIX:-$DEFAULT_GCS_PREFIX}
GCS_PATH="gs://${GCS_PREFIX}"

GCR_PATH="gcr.io/istio-release"
DOCKER_HUB=${TEST_DOCKER_HUB:-$GCR_PATH}

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/docker_tag_push_lib.sh
source "${SCRIPTPATH}/docker_tag_push_lib.sh"

if [[ "${PUSH_DOCKER}" == "true" ]]; then
  add_license_to_tar_images "${DOCKER_HUB}" "${VER_STRING}" "${OUTPUT_PATH}"
  docker_push_images        "${DOCKER_HUB}" "${VER_STRING}" "${OUTPUT_PATH}"
fi

# preserve the source from the root of the code
pushd "${ROOT}/../../../.."
tar -czf "${OUTPUT_PATH}/source.tar.gz" go src --exclude go/out
popd
gsutil -m cp -r "${OUTPUT_PATH}"/* "${GCS_PATH}/"
