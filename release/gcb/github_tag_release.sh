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


# github keys uses CB_GCS_GITHUB_TOKEN_FILE_PATH to find the github key file, decrypts if needed
# and sets GITHUB_KEYFILE
github_keys
[[ -z "${GITHUB_KEYFILE}" ]] && exit 1

USER_NAME="IstioReleaserBot"
USER_EMAIL="istio_releaser_bot@example.com"
DATE_STRING=$(date -u +%Y-%m-%dT%H:%M:%SZ)



function create_tag_reference() {
  local REPO
  local SHA
  REPO="$1"
  SHA="$2"

  local REQUEST_FILE
  REQUEST_FILE="$(mktemp /tmp/github.request.XXXX)"
  local RESPONSE_FILE
  RESPONSE_FILE="$(mktemp /tmp/github.response.XXXX)"

  # STEP 1: create an annotated tag.
cat << EOF > "${REQUEST_FILE}"
{
  "tag": "${CB_VERSION}",
  "message": "Istio Release ${CB_VERSION}",
  "object": "${SHA}",
  "type": "commit",
  "tagger": {
    "name": "${USER_NAME}",
    "email": "${USER_EMAIL}",
    "date": "${DATE_STRING}"
  }
}
EOF

  # disabling command tracing during curl call so token isn't logged
  set +o xtrace
  TOKEN=$(< "$GITHUB_KEYFILE")
  curl -s -S -X POST -o "${RESPONSE_FILE}" -H "Accept: application/vnd.github.v3+json" --retry 3 \
    -H "Content-Type: application/json" -T "${REQUEST_FILE}" -H "Authorization: token ${TOKEN}" \
    "https://api.github.com/repos/${CB_GITHUB_ORG}/${REPO}/git/tags"
  set -o xtrace

  # parse the sha from (note other URLs also present):
  # "url": "https://api.github.com/repos/:user/:repo/git/tags/d3309a0bf813bb5960a9d40245f71f129b471d33",
  # but not from:
  # "url": "https://api.github.com/repos/:user/:repo/git/commits/9737165d9451c289d8e42f0fb03137f9030d4541"
  # it's safer to distinguish the two "url" fields than the two "sha" fields

  local TAG_SHA
  TAG_SHA=$(parse_json_for_url_hex_suffix "${RESPONSE_FILE}" "url" "/git/tags")
  if [[ -z "${TAG_SHA}" ]]; then
    echo "Did not find SHA for created tag ${CB_VERSION}"
    cat "${REQUEST_FILE}"
    cat "${RESPONSE_FILE}"
    exit 1
  fi

  echo "Created annotated tag ${CB_VERSION} for SHA ${SHA} on ${CB_GITHUB_ORG}/${REPO}, result is ${TAG_SHA}"

  # STEP 2: create a reference from the tag
cat << EOF > "${REQUEST_FILE}"
{
  "ref": "refs/tags/${CB_VERSION}",
  "sha": "${TAG_SHA}"
}
EOF

  # disabling command tracing during curl call so token isn't logged
  set +o xtrace
  curl -s -S -X POST -o "${RESPONSE_FILE}" -H "Accept: application/vnd.github.v3+json" --retry 3 \
    -H "Content-Type: application/json" -T "${REQUEST_FILE}" -H "Authorization: token ${TOKEN}" \
    "https://api.github.com/repos/${CB_GITHUB_ORG}/${REPO}/git/refs"
  set -o xtrace

  local REF
  REF=$(parse_json_for_string "${RESPONSE_FILE}" "ref")
  if [[ -z "${REF}" ]]; then
    echo "Did not find REF for created ref ${CB_VERSION}"
    cat "${REQUEST_FILE}"
    cat "${RESPONSE_FILE}"
    exit 1
  fi

  rm "${REQUEST_FILE}"
  rm "${RESPONSE_FILE}"
}


echo "Beginning tag of github"
ORG_REPOS=(api istio proxy)

for GITREPO in "${ORG_REPOS[@]}"; do
  SHA=$(grep "$GITREPO" "$BUILD_FILE"  | cut -f 2 -d " ")
  if [[ -n "${SHA}" ]]; then
    create_tag_reference "${GITREPO}" "${SHA}"
  else
    echo "Did not find SHA for repo ${GITREPO}"
  fi
done
echo "Completed tag of github"
