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

# This script creates a release on github and uploads tar.gz/zip artifacts
# for the release.  The release is created in draft/pre-release form, so
# someone will likely need to visit the website to add release-notes and
# then take the release out of draft/pre-release status.
#
# This script presumes that a tag for the release was previously created
# on github (e.g., by running create_tag_reference.sh).
#
# This script relies on artifacts being in a local directory.

set -o errexit
set -o nounset
set -o pipefail
set -x

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ORG="istio"
REPO="istio"
KEYFILE=""
TOKEN=""
SHA=""
VERSION=""
REQUEST_FILE="$(mktemp /tmp/github.request.XXXX)"
RESPONSE_FILE="$(mktemp /tmp/github.response.XXXX)"
UPLOAD_DIR=""

# shellcheck source=release/json_parse_shared.sh
source "${SCRIPTPATH}/json_parse_shared.sh"

function usage() {
  echo "$0
    -k <file>  file that contains github user token
    -o <name>  github org of repo (default is \"${ORG}\")
    -r <name>  github repo name on which to create the release (default is \"${REPO}\")
    -s <sha>   commit hash to use for release
    -t <tok>   github user token to use in REST calls (use this or -k)
    -u <dir>   source directory from which to upload artifacts (optional)
    -v <ver>   version name for release"
  exit 1
}

while getopts k:o:r:s:t:u:v: arg ; do
  case "${arg}" in
    k) KEYFILE="${OPTARG}";;
    o) ORG="${OPTARG}";;
    r) REPO="${OPTARG}";;
    s) SHA="${OPTARG}";;
    t) TOKEN="${OPTARG}";;
    u) UPLOAD_DIR="${OPTARG}";;
    v) VERSION="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${ORG}" ]] && usage
[[ -z "${REPO}" ]] && usage
[[ -z "${TOKEN}" ]] && [[ -z "${KEYFILE}" ]] && usage
[[ -z "${SHA}" ]] && usage
[[ -z "${VERSION}" ]] && usage

if [[ -n "${KEYFILE}" ]]; then
  if [ ! -f "${KEYFILE}" ]; then
    echo "specified key file ${KEYFILE} does not exist"
    usage
  fi
fi


DRAFT_ARTIFACTS="[ARTIFACTS](http://gcsweb.istio.io/gcs/istio-release/releases/${VERSION}/)\\n"
DRAFT_ARTIFACTS+="* [istio-sidecar.deb](https://storage.googleapis.com/istio-release/releases/${VERSION}/deb/istio-sidecar.deb)\\n"
DRAFT_ARTIFACTS+="* [istio-sidecar.deb.sha256](https://storage.googleapis.com/istio-release/releases/${VERSION}/deb/istio-sidecar.deb.sha256)\\n\\n"
DRAFT_ARTIFACTS+="[RELEASE NOTES](https://istio.io/about/notes/${VERSION}.html)"

cat << EOF > "${REQUEST_FILE}"
{
  "tag_name": "${VERSION}",
  "target_commitsh": "${SHA}",
  "body": "${DRAFT_ARTIFACTS}",
  "draft": true,
  "prerelease": true
}
EOF

cat "${REQUEST_FILE}"

# disabling command tracing during curl call so token isn't logged
set +o xtrace
TOKEN=$(< "$KEYFILE")
curl -s -S -X POST -o "${RESPONSE_FILE}" -H "Accept: application/vnd.github.v3+json" -H "Content-Type: application/json" \
     --retry 3 -T "${REQUEST_FILE}" -H "Authorization: token ${TOKEN}" "https://api.github.com/repos/${ORG}/${REPO}/releases"
set -o xtrace

# parse ID from "url": "https://api.github.com/repos/:user/:repo/releases/8576148",
RELEASE_ID=$(parse_json_for_url_int_suffix "${RESPONSE_FILE}" "url" "/releases")
if [[ -z "${RELEASE_ID}" ]]; then
  echo "Did not find ID for created release ${VERSION}"
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
  # $2 is directory
  # $3 is mime type
  # $4 is file extension
  local FILE=""

  for FILE in "${2}"/istio-*."${4}"; do
    local BASE_NAME
    BASE_NAME=$(basename "$FILE")

    # if no directory or directory has no matching files
    if [[ "${BASE_NAME%.*}" == "*" ]]; then
      return 0
    fi
    echo Uploading "${3}:" "$FILE"
    upload_file "${1}" "${3}" "$FILE"
  done

  return 0
}

if [[ -n "${UPLOAD_DIR}" ]]; then

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

  upload_directory "${UPLOAD_URL_BASE}" "${UPLOAD_DIR}" "text/plain" "sha256"
  upload_directory "${UPLOAD_URL_BASE}" "${UPLOAD_DIR}" "application/gzip" "gz"
  upload_directory "${UPLOAD_URL_BASE}" "${UPLOAD_DIR}" "application/zip" "zip"
fi

echo "Done creating release, ID is ${RELEASE_ID}"

rm "$REQUEST_FILE"
rm "$RESPONSE_FILE"
