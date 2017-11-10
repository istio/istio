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

set -o nounset

PROJECT_ID=""
KEY_FILE_PATH=""
SVC_ACCT=""
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
TEMP_DIR="$(mktemp -d /tmp/build.temprepo.XXXX)"
TEMPLATE="$(mktemp /tmp/build.template.XXXX)"
BUILD_FILE="$(mktemp /tmp/build.request.XXXX)"
RESULT_FILE="$(mktemp /tmp/build.response.XXXX)"
VER_STRING="0.0.0"
REPO=""
REPO_FILE=default.xml
REPO_FILE_VER=master
GCR_BUCKET=""
GCS_BUCKET=""
GCR_PATH="builds/"
GCS_PATH=${GCR_PATH}
APPEND_VER_TO_GCS_PATH="false"
APPEND_VER_TO_GCR_PATH="false"
WAIT_FOR_RESULT="false"

function usage() {
  echo "$0                                                                                                                           
    -p <name> project ID                                        (required)
    -a        service account for login                         (optional, defaults to project's cloudbuild@ )
    -k <file> path to key file for service account              (required)
    -v <ver>  version string                                    (optional, defaults to $VER_STRING )
    -u <url>  URL to git repo with manifest file                (required)
    -m <file> name of manifest file in repo specified by -u     (optional, defaults to $REPO_FILE )
    -t <tag>  commit tag or branch for manifest repo in -u      (optional, defaults to $REPO_FILE_VER )
    -r <name> GCR bucket to store build artifacts               (required)
    -s <name> GCS bucket to store build artifacts               (required)
    -b <path> path where to store artifacts in GCR              (optional, defaults to $GCR_PATH )
    -c <path> path where to store artifacts in GCS              (optional, defaults to $GCS_PATH )
    -d        append -v version to path for -b                  (optional)
    -e        append -v version to path for -c                  (optional)
    -w        specify that script should wait until build done  (optional)"
  exit 1
}

while getopts a:b:c:dek:m:p:r:s:t:u:v:w arg ; do
  case "${arg}" in
    a) SVC_ACCT="${OPTARG}";;
    b) GCR_PATH="${OPTARG}";;
    c) GCS_PATH="${OPTARG}";;
    d) APPEND_VER_TO_GCR_PATH="true";;
    e) APPEND_VER_TO_GCS_PATH="true";;
    k) KEY_FILE_PATH="${OPTARG}";;
    m) REPO_FILE="${OPTARG}";;
    p) PROJECT_ID="${OPTARG}";;
    r) GCR_BUCKET="${OPTARG}";;
    s) GCS_BUCKET="${OPTARG}";;
    t) REPO_FILE_VER="${OPTARG}";;
    u) REPO="${OPTARG}";;
    v) VER_STRING="${OPTARG}";;
    w) WAIT_FOR_RESULT="true";;
    *) usage;;
  esac
done

[[ -z "${PROJECT_ID}"    ]] && usage
[[ -z "${KEY_FILE_PATH}" ]] && usage
[[ -z "${REPO}"          ]] && usage
[[ -z "${REPO_FILE}"     ]] && usage
[[ -z "${REPO_FILE_VER}" ]] && usage
[[ -z "${VER_STRING}"    ]] && usage
[[ -z "${GCS_BUCKET}"    ]] && usage
[[ -z "${GCR_BUCKET}"    ]] && usage

DEFAULT_SVC_ACCT="cloudbuild@${PROJECT_ID}.iam.gserviceaccount.com"

if [[ -z "${SVC_ACCT}"  ]]; then
  SVC_ACCT="${DEFAULT_SVC_ACCT}"
fi

if [[ "${APPEND_VER_TO_GCS_PATH}" == "true" ]]; then
  if [[ -z "${GCS_PATH}"  ]]; then
    GCS_PATH="${VER_STRING}"
  else
    GCS_PATH="${GCS_PATH}/${VER_STRING}"
  fi
fi

if [[ "${APPEND_VER_TO_GCR_PATH}" == "true" ]]; then
  if [[ -z "${GCR_PATH}"  ]]; then
    GCR_PATH="${VER_STRING}"
  else
    GCR_PATH="${GCR_PATH}/${VER_STRING}"
  fi
fi

# grab a copy of the manifest
curl -s -L -o "${TEMP_DIR}/repo" https://storage.googleapis.com/git-repo-downloads/repo
chmod u+x "${TEMP_DIR}/repo"

# grab a copy of the istio repo (though we only care about one file)
ISTIO_REPO_PATH="go/src/istio.io/istio"
pushd ${TEMP_DIR}
./repo init -u "${REPO}" -m "${REPO_FILE}" -b "${REPO_FILE_VER}"
./repo sync "${ISTIO_REPO_PATH}"
popd

# grab a copy of the template file
cp "${TEMP_DIR}/${ISTIO_REPO_PATH}/bin/cloud_build.template.json" "${TEMPLATE}"
rm -rf "${TEMP_DIR}"

# generate the json file, first strip off the closing } in the last line of the template
head --lines=-1 "${TEMPLATE}" > "${BUILD_FILE}"
echo "  \"substitutions\": {" >> "${BUILD_FILE}"
echo "    \"_VER_STRING\": \"${VER_STRING}\"," >> "${BUILD_FILE}"
echo "    \"_MFEST_URL\": \"${REPO}\"," >> "${BUILD_FILE}"
echo "    \"_MFEST_FILE\": \"${REPO_FILE}\"," >> "${BUILD_FILE}"
echo "    \"_MFEST_VER\": \"${REPO_FILE_VER}\"," >> "${BUILD_FILE}"
echo "    \"_GCS_BUCKET\": \"${GCS_BUCKET}\"," >> "${BUILD_FILE}"
echo "    \"_GCS_SUBDIR\": \"${GCS_PATH}\"," >> "${BUILD_FILE}"
echo "    \"_GCR_BUCKET\": \"${GCR_BUCKET}\"," >> "${BUILD_FILE}"
echo "    \"_GCR_SUBDIR\": \"${GCR_PATH}\"" >> "${BUILD_FILE}"
echo "  }" >> "${BUILD_FILE}"
echo "}" >> "${BUILD_FILE}"

gcloud auth activate-service-account "${SVC_ACCT}" --key-file="${KEY_FILE_PATH}"
curl -X POST -H "Authorization: Bearer $(gcloud auth print-access-token)" -T "${BUILD_FILE}" \
  -s -o "${RESULT_FILE}" https://cloudbuild.googleapis.com/v1/projects/${PROJECT_ID}/builds

# cleanup
rm -f "${TEMPLATE}"
rm -f "${BUILD_FILE}"

BUILD_ID=""
ID_WORD="id"
BUILD_ID_COUNT=$(grep -c -Eo " *\"${ID_WORD}\":.*?[^\\\\]\",?" $RESULT_FILE)
if [ "$BUILD_ID_COUNT" != "0" ]; then
  BUILD_ID=$(grep -Eo " *\"${ID_WORD}\":.*?[^\\\\]\",?" $RESULT_FILE | \
    sed "s/ *\"${ID_WORD}\": *\"\(.*\)\",*/\1/")
  echo "BUILD_ID is ${BUILD_ID}"
else
  echo "failed to parse the following build result:"
  cat $RESULT_FILE
  exit 1
fi

# parse_result_file(): parses the result from a build query.
# returns 0 if build successful, 1 if build still running, or 2 if build failed
# first input parameter is path to file to parse
function parse_result_file {
  local INPUT_FILE="$1"

  local ERROR_WORD="error"
  local ERROR_CODE_WORD="code"
  local ERROR_MSG_WORD="message"
  local STATUS_WORD="status"
  local QUEUED_WORD="QUEUED"
  local WORKING_WORD="WORKING"
  local FAILED_WORD="FAILURE"
  local SUCCESS_WORD="SUCCESS"

  [[ -z "${INPUT_FILE}" ]] && usage

  local STATUS_VALUE=""
  local STATUS_COUNT=$(grep -c -Eo " *\"${STATUS_WORD}\":.*?[^\\\\]\",?" $INPUT_FILE)
  if [ "$STATUS_COUNT" != "0" ]; then
    STATUS_VALUE=$(grep -Eo " *\"${STATUS_WORD}\":.*?[^\\\\]\",?" $INPUT_FILE | \
      sed "s/ *\"${STATUS_WORD}\": *\"\(.*\)\",*/\1/")
  fi

  local ERROR_VALUE=""
  local ERROR_CODE=""
  local ERROR_STATUS=""
  local ERROR_COUNT=$(grep -c -Eo " *\"${ERROR_WORD}\":.*{" $INPUT_FILE)
  if [ "$ERROR_COUNT" != "0" ]; then
    ERROR_CODE=$(grep -Eo " *\"${ERROR_CODE_WORD}\":.*?[^\\\\],?" $INPUT_FILE | \
      sed "s/ *\"${ERROR_CODE_WORD}\": *\([0-9]*\),*/\1/")
    ERROR_VALUE=$(grep -Eo " *\"${ERROR_MSG_WORD}\":.*?[^\\\\]\",?" $INPUT_FILE | \
      sed "s/ *\"${ERROR_MSG_WORD}\": *\"\(.*\)\",*/\1/")
    ERROR_STATUS=${STATUS_VALUE}
    STATUS_VALUE="ERROR"
  fi

  case "${STATUS_VALUE}" in
    ERROR)
      echo "build has error code ${ERROR_CODE} with \"${ERROR_STATUS}\" and \"${ERROR_VALUE}\""
      return 2
      ;;
    FAILURE)
      echo "build has failed"
      return 2
      ;;
    CANCELLED)
      echo "build was cancelled"
      return 2      
      ;;
    QUEUED)
      echo "build is queued"
      return 1
      ;;
    WORKING)
      echo "build is running at $(date)"
      return 1
      ;;
    SUCCESS)
      echo "build is successful"
      return 0
      ;;
    *)
      echo "unrecognized status: ${STATUS_VALUE}"
      return 2
  esac
}

if [[ "${WAIT_FOR_RESULT}" == "true" ]]; then
  echo "waiting for build to complete"

  curl -H "Authorization: Bearer $(gcloud auth print-access-token)" -s \
    -o "${RESULT_FILE}" "https://cloudbuild.googleapis.com/v1/projects/${PROJECT_ID}/builds/{$BUILD_ID}"

  parse_result_file "${RESULT_FILE}"
  PARSE_RESULT=$?
  while [ $PARSE_RESULT -eq 1 ]; do
    sleep 60

    curl -H "Authorization: Bearer $(gcloud auth print-access-token)" -s \
      -o "${RESULT_FILE}" "https://cloudbuild.googleapis.com/v1/projects/${PROJECT_ID}/builds/{$BUILD_ID}"

    parse_result_file "${RESULT_FILE}"
    PARSE_RESULT=$?
  done
  rm -f "${RESULT_FILE}"
  exit $PARSE_RESULT
fi

rm -f "${RESULT_FILE}"
