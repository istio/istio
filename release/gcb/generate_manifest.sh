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
# shellcheck disable=SC1091

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck disable=SC1090
source "${SCRIPTPATH}/gcb_lib.sh"

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

# also sets ISTIO_HEAD_SHA variables
function istio_checkout_green_sha() {
  pushd istio
    ISTIO_HEAD_SHA=$(git rev-parse HEAD)
    # CB_COMMIT now has the sha of branch, or branch name
    git checkout "${CB_COMMIT}"
  popd
}

# sets TOOLS_HEAD_SHA variables
function istio_tools_get_green_sha() {
  pushd tools
    TOOLS_HEAD_SHA=$(git rev-parse HEAD)
    export TOOLS_HEAD_SHA
  popd
}

function istio_check_green_sha_age() {
  pushd istio
    if [[ "${CB_CHECK_GREEN_SHA_AGE}" == "true" ]]; then
      local TS_SHA
      TS_SHA=$( git show -s --format=%ct "${CB_COMMIT}")
      local TS_HEAD
      TS_HEAD=$(git show -s --format=%ct "${ISTIO_HEAD_SHA}")
      local DIFF_SEC
      DIFF_SEC=$((TS_HEAD - TS_SHA))
      local DIFF_DAYS
      DIFF_DAYS=$((DIFF_SEC/86400))

      if [ "$DIFF_DAYS" -gt "2" ]; then
         echo ERROR: "${CB_COMMIT}" is "$DIFF_DAYS" days older than head of branch "${CB_BRANCH}"
         exit 12
      fi
    fi
  popd
}

CLONE_DIR=$(mktemp -d)
pushd "${CLONE_DIR}"
  MANIFEST_FILE="/workspace/manifest.txt"
  BASE_MANIFEST_URL="gs://${CB_GCS_BUILD_BUCKET}/release-tools/${CB_BRANCH}-manifest.txt"
  BASE_MASTER_MANIFEST_URL="gs://${CB_GCS_BUILD_BUCKET}/release-tools/master-manifest.txt"

  NEW_BRANCH="false"
  gsutil -q cp "${BASE_MANIFEST_URL}" "$MANIFEST_FILE"  || NEW_BRANCH="true"
  if [[ "${NEW_BRANCH}" == "true" ]]; then
   if [[ "${CB_COMMIT}" == "" ]]; then
     CB_COMMIT="HEAD" # just use head of branch as green sha
   fi
   gsutil -q cp "${BASE_MASTER_MANIFEST_URL}" "$MANIFEST_FILE"
  fi

  # Tools repo contains performance tests.
  git clone "https://github.com/${CB_GITHUB_ORG}/tools"
  istio_tools_get_green_sha

  git clone "https://github.com/${CB_GITHUB_ORG}/istio" -b "${CB_BRANCH}"

  istio_checkout_green_sha        "${MANIFEST_FILE}"
  istio_check_green_sha_age

  pushd istio
    PROXY_REPO_SHA=$(grep PROXY_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
    API_REPO_SHA=$(grep "istio\.io/api" go.mod | cut -f 3 -d '-')
  popd  
  checkout_code "proxy" "${PROXY_REPO_SHA}" .
  checkout_code "api" "${API_REPO_SHA}" .

  pushd istio
    create_manifest_check_consistency "${MANIFEST_FILE}"
  popd

  #copy the needed files
  # TODO figure out how to avoid need for copying to BASE_MANIFEST_URL
  gsutil -q cp "${MANIFEST_FILE}" "${BASE_MANIFEST_URL}"
  gsutil -q cp "${MANIFEST_FILE}" "gs://${CB_GCS_RELEASE_TOOLS_PATH}/manifest.txt"

popd # "${CLONE_DIR}"
rm -rf "${CLONE_DIR}"
