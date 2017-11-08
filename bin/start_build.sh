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

PROJECT_ID=""
KEY_FILE_PATH=""
SVC_ACCT=""
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
TEMPLATE="${SCRIPTPATH}/cloud_build.template.json"
TEMP_DIR="$(mktemp -d /tmp/istio.build.XXXX)"
BUILD_FILE="${TEMP_DIR}/cloud_build.json"
RESULT_FILE="${TEMP_DIR}/result.json"
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
    -e        append -v version to path for -c                  (optional)"
  exit 1
}

while getopts a:b:c:dek:m:p:r:s:t:u:v: arg ; do
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
# GCR_PATH can be empty
# GCS_PATH can be . to represent empty
[[ -z "${GCS_PATH}"      ]] && usage

DEFAULT_SVC_ACCT="cloudbuild@${PROJECT_ID}.iam.gserviceaccount.com"

if [[ -z "${SVC_ACCT}"  ]]; then
  SVC_ACCT="${DEFAULT_SVC_ACCT}"
fi

if [[ "${APPEND_VER_TO_GCS_PATH}" == "true" ]]; then
  GCS_PATH="${GCS_PATH}/${VER_STRING}"
fi

if [[ "${APPEND_VER_TO_GCR_PATH}" == "true" ]]; then
  if [[ -z "${GCR_PATH}"  ]]; then
    GCR_PATH="${VER_STRING}"
  else
    GCR_PATH="${GCR_PATH}/${VER_STRING}"
  fi
fi

## generate the json file, first strip off the closing } in the last line of the template
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
curl -X POST -H "Authorization: Bearer $(gcloud auth print-access-token)" -T "${BUILD_FILE}" -o "${RESULT_FILE}" https://cloudbuild.googleapis.com/v1/projects/${PROJECT_ID}/builds
CURL_RESULT=$?
echo curl result: $CURL_RESULT

# ${RESULT_FILE}

echo "REQUEST:"
cat "${BUILD_FILE}"
echo "RESPONSE:"
cat "${RESULT_FILE}"

# example error:
# {
#   "error": {
#     "code": 400,
#     "message": "Invalid JSON payload received. Unexpected end of string. Expected , or } after key:value pair.\n\n^",
#     "status": "INVALID_ARGUMENT"
#   }
# }

# example success:
#{
#  "name": "operations/build/delco-experimental/ZTE0ODdmODUtODU4NS00NGZlLWE3ZGMtNzY1NTAyZTVhOGMw",
#  "metadata": {
#    "@type": "type.googleapis.com/google.devtools.cloudbuild.v1.BuildOperationMetadata",
#    "build": {
#      "id": "e1487f85-8585-44fe-a7dc-765502e5a8c0",
#      "status": "QUEUED",
#      "createTime": "2017-11-08T05:19:36.488754764Z",
#      "timeout": "3600s",
#      "projectId": "delco-experimental",
#      "logsBucket": "gs://504449721521.cloudbuild-logs.googleusercontent.com",
#      "options": {
#        "machineType": "N1_HIGHCPU_32"
#      },
#      "logUrl": "https://console.cloud.google.com/gcr/builds/e1487f85-8585-44fe-a7dc-765502e5a8c0?project=delco-experimental",
#      "substitutions": {
#        "_MFEST_URL": "https://github.com/mattdelco/manifest",
#        "_MFEST_FILE": "default.xml",
#        "_MFEST_VER": "master",
#        "_GCS_BUCKET": "delco-experimental",
#        "_GCS_SUBDIR": "builds/master/0.3.0-20171107-rc0",
#        "_GCR_BUCKET": "delco-experimental",
#        "_GCR_SUBDIR": "builds/master/0.3.0-20171107-rc0",
#        "_VER_STRING": "0.3.0-20171107-rc0"
#      }
#    }
#  }
#}
