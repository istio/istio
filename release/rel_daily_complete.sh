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


# This script downloads creates a 

GCS_STAGING_BUCKET=""
VERSION=""

function usage() {
  echo "$0
    -b <branch>       branch of the build              (required)
    -s <staging bucket> staging bucket for daily build (required)
    -v <ver>          version string for this build    (required)"
  exit 1
}

while getopts b:s:v: arg ; do
  case "${arg}" in
    b) BRANCH="${OPTARG}";;
    s) GCS_STAGING_BUCKET="${OPTARG}";;
    v) VERSION="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${BRANCH}" ]] && usage
[[ -z "${VERSION}" ]] && usage
[[ -z "${GCS_STAGING_BUCKET}" ]] && usage

# remove any trailing / for GCS
GCS_STAGING_BUCKET=${GCS_STAGING_BUCKET%/}
DAILY_HTTPS_PATH="https://storage.googleapis.com/${GCS_STAGING_BUCKET}/daily-build/${VERSION}/istio-${VERSION}-linux.tar.gz"

TEMP_FILE=$(mktemp)
echo -n "${DAILY_HTTPS_PATH}" > "${TEMP_FILE}"
cat "${TEMP_FILE}"

gsutil -m cp "${TEMP_FILE}" "gs://${GCS_STAGING_BUCKET}/daily-build/${BRANCH}-latest"
