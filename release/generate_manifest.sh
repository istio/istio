#!/bin/bash

# Copyright 2018 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.


# This script finds the green build sha, generates a corresponding
# manifest file, also copies the json and scripts files from head of branch

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb_lib.sh
source "${SCRIPTPATH}/gcb_lib.sh"


#this function replace the old sha with the correct one
# e.g. calling replace_sha_branch_repo src.proxy 8888888888888888888888888888888888888888 release-0.8 build.xml
# will change 
#  <project name="istio/proxy" path="src/proxy" revision="1111111111111111111111111111111111111111" upstream="master"/>
# to
#  <project name="istio/proxy" path="src/proxy" revision="8888888888888888888888888888888888888888" upstream="release-0.8"/>
function replace_sha_branch_repo() {
  local REPO=$1
  local NEW_SHA=$2
  local BRANCH=$3
  local MANIFEST_FILE=$4
  if [[ -z "${NEW_SHA}" ]]; then
    exit 1
  fi
  if [[ -z "${BRANCH}" ]]; then
    exit 2
  fi

# REPO="istio.io.api"
  local findstr
  findstr="\\(.*$REPO. revision=.\\)\\(.*\\)\\(upstream.*\\)"
  local repstr
  repstr="\\1${NEW_SHA}\" upstream=\"${BRANCH}\"\\/\\>"
  sed "s/$findstr/$repstr/" -i "$MANIFEST_FILE"
}

function checkout_code() {
  local REPO=$1
  local REPO_SHA=$2
  local BRANCH=$3
  local DIR=$4

  mkdir -p "${DIR}"
  pushd    "${DIR}"
    git clone "https://github.com/istio/$REPO" -b "${BRANCH}"
    pushd "${REPO}"
    git checkout "${REPO_SHA}"
    popd
  popd
}

function find_and_replace_shas_manifest() {
  local BRANCH=$1
  local MANIFEST_FILE=$2
  local VERIFY_CONSISTENCY=$3

 pushd istio
  local PROXY_REPO_SHA
  PROXY_REPO_SHA=$(grep PROXY_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
  replace_sha_branch_repo src.proxy      "${PROXY_REPO_SHA}" "${BRANCH}" "${MANIFEST_FILE}"

  local API_REPO_SHA
  API_REPO_SHA=$(grep istio.io.api -A 100 < Gopkg.lock | grep revision -m 1 | cut -f 2 -d '"')
  replace_sha_branch_repo istio.io.api   "${API_REPO_SHA}"   "${BRANCH}" "${MANIFEST_FILE}"

  local ISTIO_REPO_SHA
  ISTIO_REPO_SHA=$(git rev-parse HEAD)
  replace_sha_branch_repo istio.io.istio "${ISTIO_REPO_SHA}" "${BRANCH}" "${MANIFEST_FILE}"

  if [ -z "${ISTIO_REPO_SHA}" ] || [ -z "${API_REPO_SHA}" ] || [ -z "${PROXY_REPO_SHA}" ]; then
    echo "ISTIO_REPO_SHA:$ISTIO_REPO_SHA API_REPO_SHA:$API_REPO_SHA PROXY_REPO_SHA:$PROXY_REPO_SHA some shas not found"
    exit 8
  fi
 popd

  if [[ "${VERIFY_CONSISTENCY}" == "true" ]]; then
     checkout_code "proxy" "${BRANCH}" "${BRANCH}" .
     pushd proxy 
       PROXY_HEAD_SHA=$(git rev-parse HEAD)
       PROXY_HEAD_API_SHA=$(grep ISTIO_API istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
     popd
      if [ "$PROXY_HEAD_SHA" != "$PROXY_REPO_SHA" ]; then
        echo "inconsistent shas PROXY_HEAD_SHA         $PROXY_HEAD_SHA != $PROXY_REPO_SHA PROXY_REPO_SHA" 1>&2
        exit 16
      fi
      if [ "$PROXY_HEAD_API_SHA" != "$API_REPO_SHA" ]; then
        echo "inconsistent shas PROXY_HEAD_API_SHA $PROXY_HEAD_API_SHA !=   $API_REPO_SHA   API_REPO_SHA" 1>&2
        exit 17
      fi
      if [ "$ISTIO_HEAD_SHA" != "$ISTIO_REPO_SHA" ]; then
        echo "inconsistent shas ISTIO_HEAD_SHA         $ISTIO_HEAD_SHA != $ISTIO_REPO_SHA ISTIO_REPO_SHA" 1>&2
        exit 18
      fi
  fi
}

function get_later_sha_timestamp() {
  local GSHA
  GSHA=$1
  local MANIFEST_FILE
  MANIFEST_FILE=$2

  local SHA_MFEST
  SHA_MFEST=$(grep "istio\"" "$MANIFEST_FILE" | sed 's/.*istio. revision=.//' | sed 's/".*//')
  local TS_MFEST
  TS_MFEST=$(git show -s --format=%ct "$SHA_MFEST")
  local GTS
  GTS=$(   git show -s --format=%ct "$GSHA")
  if [   "$TS_MFEST" -gt "$GTS" ]; then
    # dont go backwards
    echo "$SHA_MFEST"
    return
  fi
  #go forward
  echo   "$GSHA"
}

function get_later_sha_revlist() {
  local GSHA
  GSHA=$1
  local MANIFEST_FILE
  MANIFEST_FILE=$2

  local SHA_MFEST
  SHA_MFEST=$(grep "istio\"" "$MANIFEST_FILE" | sed 's/.*istio. revision=.//' | sed 's/".*//')

  # if the old sha in the manifest file is wrong for some reason, use latest green sha
  if ! git rev-list "$SHA_MFEST...$GSHA" > /dev/null; then
     echo "$GSHA"
     return
  fi

  local SHA_LATEST
  SHA_LATEST=$(git rev-list "$SHA_MFEST...$GSHA" | grep "$GSHA")
  if [[ -z "${SHA_LATEST}" ]]; then
    # dont go backwards
    echo "$SHA_MFEST"
    return
  fi
  #go forward
  echo   "$SHA_LATEST"
}

#sets ISTIO_SHA variable
function get_istio_green_sha() {
  local CUR_BRANCH
  CUR_BRANCH="$1"
  local MANIFEST_FILE
  MANIFEST_FILE="$2"

 pushd "${TEST_INFRA_DIR}"
  github_keys
  set +e
  # GITHUB_KEYFILE has the github tokens
  local GREEN_SHA
  GREEN_SHA=$($githubctl --token_file="${GITHUB_KEYFILE}" --repo=istio \
                         --op=getLatestGreenSHA           --base_branch="${CUR_BRANCH}" \
                         --logtostderr)
  set -e
 popd # TEST_INFRA_DIR

 pushd istio
  if [[ -z "${GREEN_SHA}" ]]; then
    echo      "GREEN_SHA empty for branch ${CUR_BRANCH} using HEAD"
     ISTIO_SHA=$(git rev-parse HEAD)
  else
    ISTIO_SHA=$(get_later_sha_revlist "${GREEN_SHA}" "${MANIFEST_FILE}")
  fi
 popd # istio
}

# also sets ISTIO_HEAD_SHA, and ISTIO_COMMIT variables
function istio_checkout_green_sha() {
  local CUR_BRANCH
  local MANIFEST_FILE
  CUR_BRANCH="$1"
  ISTIO_COMMIT="$2"
  MANIFEST_FILE="$3"

  if [[ "${ISTIO_COMMIT}" == "" ]]; then
     get_istio_green_sha "${CUR_BRANCH}" "${MANIFEST_FILE}"
     ISTIO_COMMIT="${ISTIO_SHA}"
  fi
  pushd istio
    ISTIO_HEAD_SHA=$(git rev-parse HEAD)
    # ISTIO_COMMIT now has the sha of branch, or branch name
    git checkout "${ISTIO_COMMIT}"
  popd
}

function istio_check_green_sha_age() {
pushd istio
  if [[ "${CHECK_GREEN_SHA_AGE}" == "true" ]]; then
    local TS_SHA
    TS_SHA=$( git show -s --format=%ct "${ISTIO_COMMIT}")
    local TS_HEAD
    TS_HEAD=$(git show -s --format=%ct "${ISTIO_HEAD_SHA}")
    local DIFF_SEC
    DIFF_SEC=$((TS_HEAD - TS_SHA))
    local DIFF_DAYS
    DIFF_DAYS=$((DIFF_SEC/86400))

    if [ "$CHECK_GREEN_SHA_AGE" = "true" ] && [ "$DIFF_DAYS" -gt "2" ]; then
       echo ERROR: "${ISTIO_COMMIT}" is "$DIFF_DAYS" days older than head of branch "$BRANCH"
       exit 12
    fi
  fi
popd
}

BRANCH=""
COMMIT=""
CHECK_GREEN_SHA_AGE="false"
VERIFY_CONSISTENCY="false"
GCS_RELEASE_TOOLS_PATH=""

function usage() {
  echo "$0
    -a true/false   check green sha age                 (optional)
    -b <name>       branch                              (required)
    -c <commit/sha> commit sha or branch of istio/istio (optional, its computed)
    -r <url>        directory path to release tools     (required)
    -v <value>      verify consistency                  (optional)"
  exit 1
}

while getopts a:b:c:r:v: arg ; do
  case "${arg}" in
    a) CHECK_GREEN_SHA_AGE="${OPTARG}";;
    b) BRANCH="${OPTARG}";;
    c) COMMIT="${OPTARG}";;
    r) GCS_RELEASE_TOOLS_PATH="gs://${OPTARG}";;
    v) VERIFY_CONSISTENCY="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${BRANCH}"                 ]] && usage
[[ -z "${GCS_RELEASE_TOOLS_PATH}" ]] && usage

MANIFEST_URL="${GCS_RELEASE_TOOLS_PATH}/manifest.xml"

CLONE_DIR=$(mktemp -d)
pushd "${CLONE_DIR}"
  githubctl_setup
  BASE_MANIFEST_URL="gs://istio-release-pipeline-data/release-tools/${BRANCH}-manifest.xml"
  BASE_MASTER_MANIFEST_URL="gs://istio-release-pipeline-data/release-tools/master-manifest.xml"

  NEW_BRANCH="false"
  gsutil cp "${BASE_MANIFEST_URL}" "manifest.xml"  || NEW_BRANCH="true"
  if [[ "${NEW_BRANCH}" == "true" ]]; then
   if [[ "${COMMIT}" == "" ]]; then
     COMMIT="${BRANCH}" # just use head of branch as green sha
   fi
   gsutil cp "${BASE_MASTER_MANIFEST_URL}" "manifest.xml"
  fi
  MANIFEST_FILE="$PWD/manifest.xml"

  git clone https://github.com/istio/istio -b "${BRANCH}"
  gsutil cp istio/release/*         "${GCS_RELEASE_TOOLS_PATH}/"
  gsutil cp istio/release/airflow/* "${GCS_RELEASE_TOOLS_PATH}/airflow/"

  istio_checkout_green_sha       "${BRANCH}" "${COMMIT}" "${MANIFEST_FILE}"
  istio_check_green_sha_age
  find_and_replace_shas_manifest "${BRANCH}"             "${MANIFEST_FILE}" "$VERIFY_CONSISTENCY"

  #copy the needed files
  # TODO figure out how to avoid need for copying to BASE_MANIFEST_URL and consolidate getting
  # branch istio sha into githubctl
  gsutil cp "${MANIFEST_FILE}" "${BASE_MANIFEST_URL}"
  gsutil cp "${MANIFEST_FILE}" "${MANIFEST_URL}"
popd # "${CLONE_DIR}"
rm -rf "${CLONE_DIR}"

