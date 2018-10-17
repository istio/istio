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

source "/workspace/gcb_env.sh"

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/gcb_lib.sh
source "${SCRIPTPATH}/gcb_lib.sh"


UPLOAD_DIR="$(mktemp -d /tmp/release.XXXX)"

function usage() {
  echo "$0
    uses CB_VERSION CB_GCS_FULL_STAGING_PATH CB_GCS_GITHUB_TOKEN_FILE_PATH CB_GCS_MONTHLY_RELEASE_PATH CB_GITHUB_ORG"
  exit 1
}

[[ -z "${CB_VERSION}" ]] && usage
[[ -z "${CB_GCS_FULL_STAGING_PATH}" ]] && usage
[[ -z "${CB_GCS_GITHUB_TOKEN_FILE_PATH}" ]] && usage
[[ -z "${CB_GCS_MONTHLY_RELEASE_PATH}" ]] && usage
[[ -z "${CB_GITHUB_ORG}" ]] && usage

# github keys uses CB_GCS_GITHUB_TOKEN_FILE_PATH to find the github key file, decrypts if needed
# and sets GITHUB_KEYFILE
github_keys
[[ -z "${GITHUB_KEYFILE}" ]] && usage

echo "Downloading files from $CB_GCS_FULL_STAGING_PATH to $UPLOAD_DIR"

mkdir -p "${UPLOAD_DIR}/deb/"
cp "/workspace/manifest.txt" "${UPLOAD_DIR}/"
gsutil -q -m cp gs://"${CB_GCS_FULL_STAGING_PATH}"/deb/istio*.deb* "${UPLOAD_DIR}/deb/"
gsutil -q -m cp gs://"${CB_GCS_FULL_STAGING_PATH}"/istio-*.zip* "${UPLOAD_DIR}/"
gsutil -q -m cp gs://"${CB_GCS_FULL_STAGING_PATH}"/istio-*.gz*  "${UPLOAD_DIR}/"
echo "Finished downloading files from GCS source"

# at this point everything we need is on the local filesystem
echo "Copying to GCS destination ${CB_GCS_MONTHLY_RELEASE_PATH}"
gsutil -q -m cp "${UPLOAD_DIR}"/deb/istio*.deb* "gs://${CB_GCS_MONTHLY_RELEASE_PATH}/deb/"
echo "Done copying to GCS destination"


SHA=$(grep "istio" "/workspace/manifest.txt" | cut -f 2 -d " ")
echo "Beginning release to github using sha $SHA"
"${SCRIPTPATH}/create_github_release.sh" -o "${CB_GITHUB_ORG}" -r "istio" -k "${GITHUB_KEYFILE}" \
           -v "${CB_VERSION}" -s "$SHA" -u "${UPLOAD_DIR}"
