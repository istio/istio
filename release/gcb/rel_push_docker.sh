#!/bin/bash
# Copyright 2018 Istio Authors. All Rights Reserved.
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


# This script downloads docker tar images from GCS and pushes them to docker hub

TAG=""
DOCKER_HUBS=""

function usage() {
  echo "$0
    -h <hub>  docker hub to use, multiple hubs can be comma separated (required)
    -t <tag>  version string for tag & defaulted storage paths
    and CB_GCS_BUILD_PATH
    "
  exit 1
}

while getopts h:t: arg ; do
  case "${arg}" in
    h) DOCKER_HUBS="${OPTARG}";;
    t) TAG="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${DOCKER_HUBS}" ]] && usage
[[ -z "${TAG}"         ]] && usage

[[ -z "${CB_GCS_BUILD_PATH}" ]] && usage

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/docker_tag_push_lib.sh
source "${SCRIPTPATH}/docker_tag_push_lib.sh"

TEMP_DIR=$(mktemp -d)
mkdir -p "$TEMP_DIR/docker"
gsutil -q -m cp "gs://${CB_GCS_BUILD_PATH}"/docker/* "${TEMP_DIR}/docker"

# shellcheck disable=SC2206
DOCKER_HUB_ARR=(${DOCKER_HUBS//,/ })
for HUB in "${DOCKER_HUB_ARR=[@]}"
do
  docker_tag_images  "${HUB}" "${TAG}" "${TEMP_DIR}"
  docker_push_images "${HUB}" "${TAG}" "${TEMP_DIR}"
done
