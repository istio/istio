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

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"

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

function istio_code_init_manifest() {
  local MANIFEST_FILE=$1

  API_SHA=$(  grep "api"   "$MANIFEST_FILE" | cut -f 2 -d " ")
  PROXY_SHA=$(grep "proxy" "$MANIFEST_FILE" | cut -f 2 -d " ")
  CNI_SHA=$(  grep "cni"   "$MANIFEST_FILE" | cut -f 2 -d " ")

  checkout_code "cni" "${CNI_SHA}" "/workspace/go/src/istio.io"
  checkout_code "proxy" "${PROXY_SHA}" "/workspace/src"
  checkout_code "api"     "${API_SHA}" "/workspace/go/src/istio.io"

# istio is checkout by the initialization script in pipeline repo
# ISTIO_SHA=$(grep "istio" "$MANIFEST_FILE" | cut -f 2 -d " ")
# checkout_code "istio" "${ISTIO_SHA}" "/workspace/go/src/istio.io"
}

istio_code_init_manifest         "/workspace/manifest.txt"
exit 0
