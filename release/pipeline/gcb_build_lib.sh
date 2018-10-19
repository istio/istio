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

SCRIPTPATH=$( pwd -P )
# shellcheck source=release/gcb/json_parse_shared.sh
source "${SCRIPTPATH}/json_parse_shared.sh"

# parse_result_file(): parses the result from a build query.
# returns 1 if build successful, 0 if build still running, or 2 if build failed
# first input parameter is path to file to parse
#
# Typically this function just needs to parse json for something like:
# "status": "FAILURE"
# or
# "status": "SUCCESS"
#
# but if a request is too invalid to accept then there's a different response like:
#
# {
#   "error": {
#     "code": 400,
#     "message": "Invalid JSON payload received. Unexpected end of string.",
#     "status": "INVALID_ARGUMENT"
#   }
# }
#

BUILD_FAILED=0
# XXX this is ugly, but BUILD_FAILED is being used by calling scripts which call run_build
export BUILD_FAILED

function parse_result_file {
  local INPUT_FILE="$1"

  [[ -z "${INPUT_FILE}" ]] && usage

  local STATUS_VALUE
  STATUS_VALUE=$(parse_json_for_first_string "$INPUT_FILE" "status")
  local ERROR_VALUE=""
  local ERROR_CODE=""
  local ERROR_STATUS=""

  local ERROR_LINE
  ERROR_LINE=$(parse_json_for_int "$INPUT_FILE" "code")
  if [[ -n "$ERROR_LINE" ]]; then
    ERROR_CODE=$(parse_json_for_int "$INPUT_FILE" "code")
    ERROR_VALUE=$(parse_json_for_string "$INPUT_FILE" "message")
    ERROR_STATUS=${STATUS_VALUE}
    STATUS_VALUE="ERROR"
  fi

  case "${STATUS_VALUE}" in
    ERROR)
      echo "build has error code ${ERROR_CODE} with \"${ERROR_STATUS}\" and \"${ERROR_VALUE}\""
      BUILD_FAILED=1
      return 2
      ;;
    FAILURE)
      echo "build has failed"
      BUILD_FAILED=1
      return 2
      ;;
    CANCELLED)
      echo "build was cancelled"
      BUILD_FAILED=1
      return 2
      ;;
    QUEUED)
      echo "build is queued"
      return 0
      ;;
    WORKING)
      echo "build is running at $(date)"
      return 0
      ;;
    SUCCESS)
      echo "build is successful"
      return 1
      ;;
    *)
      echo "unrecognized status: ${STATUS_VALUE}"
      cat "$INPUT_FILE"
      BUILD_FAILED=1
      return 2
  esac
}

function run_build() {
  local TEMPLATE_NAME=$1
  local SUBS_FILE=$2
  local PROJ_ID=$3
  local SERVICE_ACCT=$4
  local WAIT="true"

  local REQUEST_FILE
  REQUEST_FILE="$(mktemp /tmp/build.request.XXXX)"
  local RESULT_FILE
  RESULT_FILE="$(mktemp /tmp/build.response.XXXX)"

  # generate the json file, first strip off the closing } in the last line of the template
  head --lines=-1 "${SCRIPTPATH}/${TEMPLATE_NAME}" > "${REQUEST_FILE}"
  cat "${SUBS_FILE}" >> "${REQUEST_FILE}"
  echo "}" >> "${REQUEST_FILE}"

  curl -X POST -H "Authorization: Bearer $(gcloud auth --account "${SERVICE_ACCT}" print-access-token)" \
    -T "${REQUEST_FILE}" -s -o "${RESULT_FILE}" "https://cloudbuild.googleapis.com/v1/projects/${PROJ_ID}/builds"

  # cleanup
  rm -f "${REQUEST_FILE}"

  # the following tries to find and parse a json line like:
  # "id": "e1487f85-8585-44fe-a7dc-765502e5a8c0",
  local BUILD_ID
  BUILD_ID=$(parse_json_for_string "$RESULT_FILE" "id")
  if [[ -z "$BUILD_ID" ]]; then
    echo "failed to parse the following build result:"
    cat "$RESULT_FILE"
    exit 1
  fi
  echo "BUILD_ID is ${BUILD_ID}"

  if [[ "${WAIT}" == "true" ]]; then
    echo "waiting for build to complete"

    while parse_result_file "${RESULT_FILE}"
    do
      sleep 60

      curl -H "Authorization: Bearer $(gcloud auth --account "${SERVICE_ACCT}" print-access-token)" -s --retry 3 \
        -o "${RESULT_FILE}" "https://cloudbuild.googleapis.com/v1/projects/${PROJ_ID}/builds/{$BUILD_ID}"
    done
  fi

  rm -f "${RESULT_FILE}"
}
