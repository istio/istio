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

# This is an example file for how to start an istio build using Cloud Builder.
# To run it you need a Google Cloud project and a service account that has
# been granted access to start a build.

set -o errexit
set -o nounset
set -o pipefail
set -x

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/airflow/gcb_build_lib.sh
source "${SCRIPTPATH}/release/airflow/gcb_build_lib.sh"

KEY_FILE_PATH=""
SUBS_FILE="$(mktemp /tmp/build.subs.XXXX)"
VER_STRING="0.0.0"
WAIT_FOR_RESULT="false"

GCR_PATH=""
GCS_PATH=""

function usage() {
  echo "$0
    -k <file> path to key file for service account              (optional)
    -v <ver>  version string                                    (optional, defaults to $VER_STRING )
    -w        specify that script should wait until build done  (optional)

    -r <name> GCR bucket/path to store build artifacts          (required)
    -s <name> GCS bucket/path to store build artifacts          (required)"
  exit 1
}

while getopts k:r:s:v:w arg ; do
  case "${arg}" in
    k) KEY_FILE_PATH="${OPTARG}";;
    r) GCR_PATH="${OPTARG}";;
    s) GCS_PATH="${OPTARG}";;
    v) VER_STRING="${OPTARG}";;
    w) WAIT_FOR_RESULT="true";;
    *) usage;;
  esac
done

[[ -z "${BRANCH}"     ]] && usage
[[ -z "${PROJECT_ID}" ]] && usage
[[ -z "${VER_STRING}" ]] && usage

[[ -z "${GCS_PATH}" ]] && usage
[[ -z "${GCR_PATH}" ]] && usage
[[ -z "${GCS_RELEASE_TOOLS_PATH}" ]] && usage

DEFAULT_SVC_ACCT="cloudbuild@${PROJECT_ID}.iam.gserviceaccount.com"

if [[ -z "${SVC_ACCT}" ]]; then
  SVC_ACCT="${DEFAULT_SVC_ACCT}"
fi

# generate the substitutions file
cat << EOF > "${SUBS_FILE}"
  "substitutions": {
    "_BRANCH": "${BRANCH}",
    "_VER_STRING": "${VER_STRING}",
    "_GCS_PATH": "${GCS_PATH}",
    "_GCS_RELEASE_TOOLS_PATH": "${GCS_RELEASE_TOOLS_PATH}",
    "_GCR_PATH": "${GCR_PATH}"
  }
EOF

run_build "cloud_build.template.json" \
  "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}" "${KEY_FILE_PATH}" "${WAIT_FOR_RESULT}"

# cleanup
rm -f "${SUBS_FILE}"
exit "$BUILD_FAILED"
