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

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/gcb_lib.sh
source "${SCRIPTPATH}/gcb_lib.sh"

# this function replace the old sha with the correct one
# each line is assumed to be of the form
# proxy 8888888888888888888888888888888888888888
function replace_sha_branch_repo() {
  local REPO=$1
  local NEW_SHA=$2
  local MANIFEST_FILE=$3
  if [[ -z "${NEW_SHA}" ]]; then
    exit 1
  fi
  if [[ -z "${CB_BRANCH}" ]]; then
    exit 2
  fi
  sed "s/$REPO.*/$REPO $NEW_SHA/" -i "$MANIFEST_FILE"
}

function checkout_code() {
  local REPO=$1
  local REPO_SHA=$2
  local DIR=$3

  mkdir -p "${DIR}"
  pushd    "${DIR}"
    git clone "https://github.com/${CB_GITHUB_ORG}/$REPO" -b "${CB_BRANCH}"
    pushd "${REPO}"
    git checkout "${REPO_SHA}"
    popd
  popd
}

function find_and_replace_shas_manifest() {
  local MANIFEST_FILE=$1

 pushd istio
  local PROXY_REPO_SHA
  PROXY_REPO_SHA=$(grep PROXY_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
  replace_sha_branch_repo proxy "${PROXY_REPO_SHA}" "${MANIFEST_FILE}"

  local API_REPO_SHA
  API_REPO_SHA=$(grep istio.io.api -A 100 < Gopkg.lock | grep revision -m 1 | cut -f 2 -d '"')
  replace_sha_branch_repo api   "${API_REPO_SHA}"   "${MANIFEST_FILE}"

  local ISTIO_REPO_SHA
  ISTIO_REPO_SHA=$(git rev-parse HEAD)
  replace_sha_branch_repo istio "${ISTIO_REPO_SHA}" "${MANIFEST_FILE}"

  if [ -z "${ISTIO_REPO_SHA}" ] || [ -z "${API_REPO_SHA}" ] || [ -z "${PROXY_REPO_SHA}" ]; then
    echo "ISTIO_REPO_SHA:$ISTIO_REPO_SHA API_REPO_SHA:$API_REPO_SHA PROXY_REPO_SHA:$PROXY_REPO_SHA some shas not found"
    exit 8
  fi
 popd

  if [[ "${CB_VERIFY_CONSISTENCY}" == "true" ]]; then
     checkout_code "proxy" "HEAD" .
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
  SHA_MFEST=$(grep "istio" "$MANIFEST_FILE" | cut -f 2 -d " ")

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
  local MANIFEST_FILE
  MANIFEST_FILE="$1"

 pushd "${TEST_INFRA_DIR}"
  github_keys
  set +e
  # GITHUB_KEYFILE has the github tokens
  local GREEN_SHA
  GREEN_SHA=$($githubctl --token_file="${GITHUB_KEYFILE}" --repo=istio \
                         --op=getLatestGreenSHA           --base_branch="${CB_BRANCH}" \
                         --logtostderr)
  set -e
 popd # TEST_INFRA_DIR

 pushd istio
  if [[ -z "${GREEN_SHA}" ]]; then
    echo      "GREEN_SHA empty for branch ${CB_BRANCH} using HEAD"
     ISTIO_SHA=$(git rev-parse HEAD)
  else
    ISTIO_SHA=$(get_later_sha_revlist "${GREEN_SHA}" "${MANIFEST_FILE}")
  fi
 popd # istio
}

# also sets ISTIO_HEAD_SHA, and ISTIO_COMMIT variables
function istio_checkout_green_sha() {
  local MANIFEST_FILE
  MANIFEST_FILE="$1"

  if [[ "${CB_COMMIT}" == "" ]]; then
     get_istio_green_sha "${MANIFEST_FILE}"
     ISTIO_COMMIT="${ISTIO_SHA}"
  else
     ISTIO_COMMIT="${CB_COMMIT}"
  fi
  pushd istio
    ISTIO_HEAD_SHA=$(git rev-parse HEAD)
    # ISTIO_COMMIT now has the sha of branch, or branch name
    git checkout "${ISTIO_COMMIT}"
  popd
}

function istio_check_green_sha_age() {
pushd istio
  if [[ "${CB_CHECK_GREEN_SHA_AGE}" == "true" ]]; then
    local TS_SHA
    TS_SHA=$( git show -s --format=%ct "${ISTIO_COMMIT}")
    local TS_HEAD
    TS_HEAD=$(git show -s --format=%ct "${ISTIO_HEAD_SHA}")
    local DIFF_SEC
    DIFF_SEC=$((TS_HEAD - TS_SHA))
    local DIFF_DAYS
    DIFF_DAYS=$((DIFF_SEC/86400))

    if [ "${CB_CHECK_GREEN_SHA_AGE}" = "true" ] && [ "$DIFF_DAYS" -gt "2" ]; then
       echo ERROR: "${ISTIO_COMMIT}" is "$DIFF_DAYS" days older than head of branch "${CB_BRANCH}"
       exit 12
    fi
  fi
popd
}


function usage() {
  echo "$0
        used CB_BRANCH CB_GCS_RELEASE_TOOLS_PATH CB_GITHUB_ORG CB_CHECK_GREEN_SHA_AGE CB_VERIFY_CONSISTENCY
        CB_COMMIT (optional)"
  exit 1
}

[[ -z "${CB_BRANCH}"                 ]] && usage
[[ -z "${CB_CHECK_GREEN_SHA_AGE}"    ]] && usage
[[ -z "${CB_GCS_BUILD_BUCKET}"       ]] && usage
[[ -z "${CB_GCS_RELEASE_TOOLS_PATH}" ]] && usage
[[ -z "${CB_GITHUB_ORG}"             ]] && usage
[[ -z "${CB_VERIFY_CONSISTENCY}"     ]] && usage


CLONE_DIR=$(mktemp -d)
pushd "${CLONE_DIR}"
  githubctl_setup
  gsutil -q cp -P "$githubctl" "gs://${CB_GCS_RELEASE_TOOLS_PATH}/"

  BASE_MANIFEST_URL="gs://${CB_GCS_BUILD_BUCKET}/release-tools/${CB_BRANCH}-manifest.txt"
  BASE_MASTER_MANIFEST_URL="gs://${CB_GCS_BUILD_BUCKET}/release-tools/master-manifest.txt"

  NEW_BRANCH="false"
  gsutil -q cp "${BASE_MANIFEST_URL}" "manifest.txt"  || NEW_BRANCH="true"
  if [[ "${NEW_BRANCH}" == "true" ]]; then
   if [[ "${CB_COMMIT}" == "" ]]; then
     CB_COMMIT="HEAD" # just use head of branch as green sha
   fi
   gsutil -q cp "${BASE_MASTER_MANIFEST_URL}" "manifest.txt"
  fi
  MANIFEST_FILE="$PWD/manifest.txt"

  git clone "https://github.com/${CB_GITHUB_ORG}/istio" -b "${CB_BRANCH}"
  gsutil -m -q cp -P istio/release/gcb/*sh   "gs://${CB_GCS_RELEASE_TOOLS_PATH}/"
  gsutil -m -q cp -P istio/release/airflow/* "gs://${CB_GCS_RELEASE_TOOLS_PATH}/airflow/"

  istio_checkout_green_sha        "${MANIFEST_FILE}"
  istio_check_green_sha_age
  find_and_replace_shas_manifest  "${MANIFEST_FILE}"

  #copy the needed files
  # TODO figure out how to avoid need for copying to BASE_MANIFEST_URL and consolidate getting
  # branch istio sha into githubctl
  gsutil -q cp "${MANIFEST_FILE}" "${BASE_MANIFEST_URL}"
  gsutil -q cp "${MANIFEST_FILE}" "gs://${CB_GCS_RELEASE_TOOLS_PATH}/manifest.txt"

popd # "${CLONE_DIR}"
rm -rf "${CLONE_DIR}"

