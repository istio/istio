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

# This is an example file for how to start an istio release tag
# using Cloud Builder. To run it you need a Google Cloud project and
# a service account that has been granted access to start a build.
#
# NOTE: the settings for the repo tool manifest controls which
# script version is used to perform the tag.  The shas used
# in the actual tagging is controlled by the manifest.xml stored
# in GCS (-s).  Normally the same manifest is used for both,
# though bug fixes (or a non-branching model for build automation)
# might involve the use of something newer.

set -o errexit
set -o nounset
set -o pipefail
set -x

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb_build_lib.sh
source "${SCRIPTPATH}/gcb_build_lib.sh"

PROJECT_ID=""
KEY_FILE_PATH=""
SVC_ACCT=""
SUBS_FILE="$(mktemp /tmp/build.subs.XXXX)"
VER_STRING="0.0.0"
WAIT_FOR_RESULT="false"

GCS_SRC=""
GCS_GITHUB_SECRET="istio-secrets/github.txt.enc"
REL_ORG="istio"
USER_EMAIL=""
USER_NAME=""
GCS_PATH="" #this is the initial path where the artifacts are stored before testing
BRANCH=""

function usage() {
  echo "$0
    -p <name> project ID                                        (required)
    -a        service account for login                         (optional, defaults to project's cloudbuild@ )
    -k <file> path to key file for service account              (optional)
    -v <ver>  version string                                    (optional, defaults to $VER_STRING )
    -w        specify that script should wait until build done  (optional)

    -s <name> GCS bucket to read build artifacts                (required)
    -g <path> GCS bucket&path to file with github secret        (optional, detaults to $GCS_GITHUB_SECRET )
    -h <name> github org to tag                                 (optional, defaults to $REL_ORG )
    -e <email> email of submitter for tags                      (required)
    -n <name>  name of submitter for tags                       (required)
    -c <name>  GCS bucket/path where artifacts stored pre test  (required)
    -z <name>  name of the branch                               (required)"
  exit 1
}

while getopts a:c:e:g:h:k:n:p:s:v:w:z arg ; do
  case "${arg}" in
    a) SVC_ACCT="${OPTARG}";;
    c) GCS_PATH="${OPTARG}";;
    e) USER_EMAIL="${OPTARG}";;
    g) GCS_GITHUB_SECRET="${OPTARG}";;
    h) REL_ORG="${OPTARG}";;
    k) KEY_FILE_PATH="${OPTARG}";;
    n) USER_NAME="${OPTARG}";;
    p) PROJECT_ID="${OPTARG}";;
    s) GCS_SRC="${OPTARG}";;
    v) VER_STRING="${OPTARG}";;
    w) WAIT_FOR_RESULT="true";;
    z) BRANCH="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${BRANCH}"     ]] && usage
[[ -z "${GCS_PATH}"   ]] && usage
[[ -z "${PROJECT_ID}" ]] && usage
[[ -z "${VER_STRING}" ]] && usage

[[ -z "${USER_EMAIL}" ]] && usage
[[ -z "${USER_NAME}"  ]] && usage
[[ -z "${GCS_SRC}"    ]] && usage

DEFAULT_SVC_ACCT="cloudbuild@${PROJECT_ID}.iam.gserviceaccount.com"

if [[ -z "${SVC_ACCT}"  ]]; then
  SVC_ACCT="${DEFAULT_SVC_ACCT}"
fi

# generate the substitutions file
cat << EOF > "${SUBS_FILE}"
  "substitutions": {
    "_BRANCH": "${BRANCH}",
    "_GCS_PATH": "${GCS_PATH}",
    "_VER_STRING": "${VER_STRING}",
    "_GCS_SOURCE": "${GCS_SRC}",
    "_GCS_SECRET": "${GCS_GITHUB_SECRET}",
    "_ORG": "${REL_ORG}",
    "_USER_EMAIL": "${USER_EMAIL}",
    "_USER_NAME": "${USER_NAME}"
  }
EOF

run_build "cloud_tag.template.json" \
  "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}" "${KEY_FILE_PATH}" "${WAIT_FOR_RESULT}"

# cleanup
rm -f "${SUBS_FILE}"
exit "$BUILD_FAILED"
