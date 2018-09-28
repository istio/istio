#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.


# Script to do repo init equivalent

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

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

function istio_code_init_manifest() {
  local BRANCH=$1
  local MANIFEST_FILE=$2

  ISTIO_SHA=$(grep "istio/istio" "$MANIFEST_FILE" | cut -f 6 -d \")
  API_SHA=$(  grep "istio/api"   "$MANIFEST_FILE" | cut -f 6 -d \")
  PROXY_SHA=$(grep "istio/proxy" "$MANIFEST_FILE" | cut -f 6 -d \")

  checkout_code "proxy" "${PROXY_SHA}" "${BRANCH}" "/workspace/src"
  checkout_code "api"     "${API_SHA}" "${BRANCH}" "/workspace/go/src/istio.io"
  checkout_code "istio" "${ISTIO_SHA}" "${BRANCH}" "/workspace/go/src/istio.io"
}

BRANCH=""
GCS_RELEASE_TOOLS_PATH=""

function usage() {
  echo "$0
    -b <name>       branch                              (required)
    -r <url>        directory path to release tools     (required)"
  exit 1
}

while getopts b:r: arg ; do
  case "${arg}" in
    b) BRANCH="${OPTARG}";;
    r) GCS_RELEASE_TOOLS_PATH="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${BRANCH}"                 ]] && usage
[[ -z "${GCS_RELEASE_TOOLS_PATH}" ]] && usage

MANIFEST_URL="${GCS_RELEASE_TOOLS_PATH}/manifest.xml"
gsutil cp                "gs://${MANIFEST_URL}" "/output/manifest.xml" || echo ""

MANIFEST_LOCAL_FILE="/output/manifest.xml"
gsutil cp                "gs://${MANIFEST_URL}" "${MANIFEST_LOCAL_FILE}"
istio_code_init_manifest      "${BRANCH}"       "${MANIFEST_LOCAL_FILE}"
exit 0
