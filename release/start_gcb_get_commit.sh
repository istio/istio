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
# shellcheck source=release/gcb_build_lib.sh
source "${SCRIPTPATH}/gcb_build_lib.sh"

SUBS_FILE="$(mktemp /tmp/build.subs.XXXX)"
WAIT_FOR_RESULT="true"

[[ -z "${SVC_ACCT}"   ]] && usage
[[ -z "${PROJECT_ID}" ]] && usage
[[ -z "${BRANCH}"     ]] && usage
[[ -z "${GCS_RELEASE_TOOLS_PATH}" ]] && usage
[[ -z "${VERIFY_CONSISTENCY}"     ]] && usage

# "_COMMIT":"" KPTD TODO fix this
# generate the substitutions file
cat << EOF > "${SUBS_FILE}"
  "substitutions": {
    "_BRANCH": "${BRANCH}",
    "_CHECK_GREEN_SHA_AGE": "${CHECK_GREEN_SHA_AGE}"
    "_COMMIT": ""
    "_GCS_RELEASE_TOOLS_PATH": "${GCS_RELEASE_TOOLS_PATH}"
    "_VERIFY_CONSISTENCY": "${VERIFY_CONSISTENCY}",
  }
EOF

KEY_FILE_PATH=""

run_build "cloud_build.template.json" \
  "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}" "${KEY_FILE_PATH}" "${WAIT_FOR_RESULT}"

# cleanup
rm -f "${SUBS_FILE}"
exit "$BUILD_FAILED"
