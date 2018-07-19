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

# This script creates a tags / references on github using the repos and
# shas that are cited in an xml file used with the repo tool.
#
# This script relies on the file being in the local directory.  If you'd
# instead like to specify a GCS source then consider running this script
# via publish_release.sh instead (don't forget to disable the other steps
# in that script like releasing to gcs/gcr/docker, etc.).

set -o errexit
set -o nounset
set -o pipefail
set -x

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )

KEYFILE=""
TOKEN=""

VERSION=""
PRODUCT="Istio"
DATE_STRING=$(date -u +%Y-%m-%dT%H:%M:%SZ)
USER_EMAIL=""
USER_NAME=""
BUILD_FILE=""
ORG="istio"

source ${SCRIPTPATH}/json_parse_shared.sh

function usage() { 
  echo "$0
    -b <build> repo xml file used to create release
    -d <date>  date of release (optional, defaults to current ${DATE_STRING} )
    -e <email> email of submitter
    -k <file>  file that contains github user token
    -n <name>  name of submitter
    -o <org>   specifies org to tag (optional, defaults to ${ORG} )
    -p <name>  name of product (optional, defaults to ${PRODUCT} )
    -t <tok>   github user token to use in REST calls (use this or -k)
    -v <ver>   version tag of release"
  exit 1
}

while getopts b:d:e:k:n:o:p:t:v: arg ; do
  case "${arg}" in
    b) BUILD_FILE="${OPTARG}";;
    d) DATE_STRING="${OPTARG}";;
    e) USER_EMAIL="${OPTARG}";;
    k) KEYFILE="${OPTARG}";;
    n) USER_NAME="${OPTARG}";;
    o) ORG="${OPTARG}";;
    p) PRODUCT="${PRODUCT}";;
    t) TOKEN="${OPTARG}";;
    v) VERSION="${OPTARG}";;
    *) usage;; 
  esac
done 

[[ ! -f "${BUILD_FILE}" ]] && usage
[[ -z "${DATE_STRING}" ]] && usage
[[ -z "${ORG}" ]] && usage
[[ -z "${PRODUCT}" ]] && usage
[[ -z "${TOKEN}" ]] && [[ -z "${KEYFILE}" ]] && usage
[[ -z "${VERSION}" ]] && usage
# I tried using /user to automatically get name and email, but they're both null
[[ -z "${USER_NAME}" ]] && usage
[[ -z "${USER_EMAIL}" ]] && usage

if [[ -n "${KEYFILE}" ]]; then
  if [ ! -f "${KEYFILE}" ]; then
    echo "specified key file ${KEYFILE} does not exist"
    usage
  fi
fi

function create_tag_reference() {
  # $1 ORG
  # $2 REPO
  # $3 SHA
  # uses external PRODUCT, VERSION, USER_NAME, USER_EMAIL, DATE_STRING
  
  local REQUEST_FILE="$(mktemp /tmp/github.request.XXXX)"
  local RESPONSE_FILE="$(mktemp /tmp/github.response.XXXX)"

  # STEP 1: create an annotated tag.
cat << EOF > ${REQUEST_FILE}
{
  "tag": "${VERSION}",
  "message": "${PRODUCT} Release ${VERSION}",
  "object": "${3}",
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
  TOKEN=$(< $KEYFILE)
  curl -s -S -X POST -o ${RESPONSE_FILE} -H "Accept: application/vnd.github.v3+json" --retry 3 \
    -H "Content-Type: application/json" -T ${REQUEST_FILE} -H "Authorization: token ${TOKEN}" \
    "https://api.github.com/repos/${1}/${2}/git/tags"
  set -o xtrace

  # parse the sha from (note other URLs also present):
  # "url": "https://api.github.com/repos/:user/:repo/git/tags/d3309a0bf813bb5960a9d40245f71f129b471d33",
  # but not from:
  # "url": "https://api.github.com/repos/:user/:repo/git/commits/9737165d9451c289d8e42f0fb03137f9030d4541"
  # it's safer to distinguish the two "url" fields than the two "sha" fields

  local TAG_SHA=$(parse_json_for_url_hex_suffix ${RESPONSE_FILE} "url" "/git/tags")
  if [[ -z "${TAG_SHA}" ]]; then
    echo "Did not find SHA for created tag ${VERSION}"
    cat ${REQUEST_FILE}
    cat ${RESPONSE_FILE}
    exit 1
  fi

  echo "Created annotated tag ${VERSION} for SHA ${3} on ${1}/${2}, result is ${TAG_SHA}"

  # STEP 2: create a reference from the tag
cat << EOF > ${REQUEST_FILE}
{
  "ref": "refs/tags/${VERSION}",
  "sha": "${TAG_SHA}"
}
EOF

  # disabling command tracing during curl call so token isn't logged
  set +o xtrace
  curl -s -S -X POST -o ${RESPONSE_FILE} -H "Accept: application/vnd.github.v3+json" --retry 3 \
    -H "Content-Type: application/json" -T ${REQUEST_FILE} -H "Authorization: token ${TOKEN}" \
    "https://api.github.com/repos/${1}/${2}/git/refs"
  set -o xtrace

  local REF=$(parse_json_for_string ${RESPONSE_FILE} "ref")
  if [[ -z "${REF}" ]]; then
    echo "Did not find REF for created ref ${VERSION}"
    cat ${REQUEST_FILE}
    cat ${RESPONSE_FILE}
    exit 1
  fi
  
  rm ${REQUEST_FILE}
  rm ${RESPONSE_FILE}
}


# eventually this can be used on all of:
# (istio/api istio/istio istio/proxy istio/vendor-istio)
# ORG_REPOS=(api istio proxy vendor-istio)

ORG_REPOS=(api istio proxy)

for GITREPO in ${ORG_REPOS[@]}; do
  SHA=`grep $ORG/$GITREPO $BUILD_FILE  | cut -f 6 -d \"`
  if [[ -n "${SHA}" ]]; then
    create_tag_reference "${ORG}" "${GITREPO}" "${SHA}"
  else
    echo "Did not find SHA for ${ORG}/${GITREPO}"
  fi
done
