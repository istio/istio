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

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/gcb_lib.sh
source "${SCRIPTPATH}/gcb_lib.sh"
# shellcheck source=release/gcb/json_parse_shared.sh
source "${SCRIPTPATH}/json_parse_shared.sh"



UPLOAD_DIR="$(mktemp -d /tmp/release.XXXX)"

# copies github key file locally, decrypts if needed and sets GITHUB_KEYFILE
github_keys
[[ -z "${GITHUB_KEYFILE}" ]] && exit 1

echo "Downloading files from $CB_GCS_FULL_STAGING_PATH to $UPLOAD_DIR"

mkdir -p "${UPLOAD_DIR}/deb/"
cp "/workspace/manifest.txt" "${UPLOAD_DIR}/"
gsutil -q -m cp gs://"${CB_GCS_FULL_STAGING_PATH}"/deb/istio*.deb* "${UPLOAD_DIR}/deb/"
gsutil -q -m cp gs://"${CB_GCS_FULL_STAGING_PATH}"/istio-*.zip* "${UPLOAD_DIR}/"
gsutil -q -m cp gs://"${CB_GCS_FULL_STAGING_PATH}"/istio-*.gz*  "${UPLOAD_DIR}/"
mkdir -p "${UPLOAD_DIR}/charts/"
gsutil -q -m cp -r gs://"${CB_GCS_FULL_STAGING_PATH}"/charts "${UPLOAD_DIR}/"
echo "Finished downloading files from GCS source"

# at this point everything we need is on the local filesystem
echo "Copying to GCS destination ${CB_GCS_MONTHLY_RELEASE_PATH}"
gsutil -q -m cp "${UPLOAD_DIR}"/deb/istio*.deb* "gs://${CB_GCS_MONTHLY_RELEASE_PATH}/deb/"
gsutil -q -m cp -r "${UPLOAD_DIR}"/charts "gs://${CB_GCS_MONTHLY_RELEASE_PATH}/charts"

echo "Done copying to GCS destination"


SHA=$(grep "istio" "/workspace/manifest.txt" | cut -f 2 -d " ")
echo "Beginning release to github using sha $SHA"

REQUEST_FILE="$(mktemp /tmp/github.request.XXXX)"
RESPONSE_FILE="$(mktemp /tmp/github.response.XXXX)"

DRAFT_ARTIFACTS="[ARTIFACTS](http://gcsweb.istio.io/gcs/istio-release/releases/${CB_VERSION}/)\\n"
DRAFT_ARTIFACTS+="* [istio-sidecar.deb](https://storage.googleapis.com/istio-release/releases/${CB_VERSION}/deb/istio-sidecar.deb)\\n"
DRAFT_ARTIFACTS+="* [istio-sidecar.deb.sha256](https://storage.googleapis.com/istio-release/releases/${CB_VERSION}/deb/istio-sidecar.deb.sha256)\\n\\n"
DRAFT_ARTIFACTS+="* [Helm Chart Index](https://storage.googleapis.com/istio-release/releases/${CB_VERSION}/charts/index.yaml)\\n\\n"
DRAFT_ARTIFACTS+="[RELEASE NOTES](https://istio.io/about/notes/${CB_VERSION}.html)"

cat << EOF > "${REQUEST_FILE}"
{
  "tag_name": "${CB_VERSION}",
  "target_commitsh": "${SHA}",
  "body": "${DRAFT_ARTIFACTS}",
  "draft": true,
  "prerelease": true
}
EOF

cat "${REQUEST_FILE}"

# disabling command tracing during curl call so token isn't logged
set +o xtrace
TOKEN=$(< "$GITHUB_KEYFILE")
curl -s -S -X POST -o "${RESPONSE_FILE}" -H "Accept: application/vnd.github.v3+json" -H "Content-Type: application/json" \
     --retry 3 -T "${REQUEST_FILE}" -H "Authorization: token ${TOKEN}" "https://api.github.com/repos/${CB_GITHUB_ORG}/istio/releases"
set -o xtrace

# parse ID from "url": "https://api.github.com/repos/:user/:repo/releases/8576148",
RELEASE_ID=$(parse_json_for_url_int_suffix "${RESPONSE_FILE}" "url" "/releases")
if [[ -z "${RELEASE_ID}" ]]; then
  echo "Did not find ID for created release ${CB_VERSION}"
  cat "${REQUEST_FILE}"
  cat "${RESPONSE_FILE}"
  exit 1
fi

echo "Created release, ID is ${RELEASE_ID}"

function upload_file {
  # externals used: TOKEN, RESPONSE_FILE
  # $1 is upload URL
  # $2 is mime type
  # $3 is file name
  local UPLOAD_BASE
  UPLOAD_BASE=$(basename "$3")
  echo "Uploading: $3"

  # disabling command tracing during curl call so token isn't logged
  set +o xtrace
  curl -s -S -X POST -o "${RESPONSE_FILE}" -H "Accept: application/vnd.github.v3+json" \
       --retry 3 -H "Content-Type: ${2}" -T "$3" -H "Authorization: token ${TOKEN}" \
       "${1}?name=$UPLOAD_BASE"
  set -o xtrace

    # "url":"https://api.github.com/repos/istio/istio/releases/assets/5389350",
    # "id":5389350,
    # "name":"istio-0.6.2-linux.tar.gz",
    # "state":"uploaded",
    # "browser_download_url":"https://github.com/istio/istio/releases/download/untagged-8a48f577969321f13491/istio-0.6.2-linux.tar.gz"
    local DOWNLOAD_URL
    DOWNLOAD_URL=$(parse_json_for_string "${RESPONSE_FILE}" "browser_download_url")
    if [[ -z "${DOWNLOAD_URL}" ]]; then
	echo "Did not find Download URL for file $3"
	cat "${RESPONSE_FILE}"
	exit 1
    fi
    echo "Download URL for file ${UPLOAD_BASE} is ${DOWNLOAD_URL}"
    return 0
}

function upload_directory() {
  # $1 is upload URL
  # $2 is mime type
  # $3 is file extension
  local FILE=""

  for FILE in "${UPLOAD_DIR}"/istio-*."${3}"; do
    local BASE_NAME
    BASE_NAME=$(basename "$FILE")

    # if no directory or directory has no matching files
    if [[ "${BASE_NAME%.*}" == "*" ]]; then
      return 0
    fi
    echo Uploading "${2}:" "$FILE"
    upload_file "${1}" "${2}" "$FILE"
  done

  return 0
}


# "upload_url": "https://uploads.github.com/repos/istio/istio/releases/8576148/assets{?name,label}",
UPLOAD_URL=$(parse_json_for_string "${RESPONSE_FILE}" "upload_url")
if [[ -z "${UPLOAD_URL}" ]]; then
  echo "Did not find Upload URL for created release ID ${RELEASE_ID}"
  cat "${REQUEST_FILE}"
  cat "${RESPONSE_FILE}"
  exit 1
fi

echo "UPLOAD_URL for release is \"${UPLOAD_URL}\""

# chop off the trailing {} part of the URL
# ${var%%pattern} Removes longest part of $pattern from the end of $var.
UPLOAD_URL_BASE=${UPLOAD_URL%%\{*\}}
if [[ -z "${UPLOAD_URL_BASE}" ]]; then
  echo "Could not parse Upload URL ${UPLOAD_URL} for created release ID ${RELEASE_ID}"
  cat "${REQUEST_FILE}"
  cat "${RESPONSE_FILE}"
  exit 1
fi

upload_directory "${UPLOAD_URL_BASE}" "text/plain" "sha256"
upload_directory "${UPLOAD_URL_BASE}" "application/gzip" "gz"
upload_directory "${UPLOAD_URL_BASE}" "application/zip" "zip"

echo "Done creating release, ID is ${RELEASE_ID}"
