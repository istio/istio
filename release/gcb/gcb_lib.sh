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


# This script is meant to be sourced, has a set of functions used by scripts on gcb

#sets GITHUB_KEYFILE to github auth file
function github_keys() {
  GITHUB_KEYFILE="${GITHUB_TOKEN_FILE}"
  export GITHUB_KEYFILE
  
  if [[ -n "$CB_TEST_GITHUB_TOKEN_FILE_PATH" ]]; then
    local LOCAL_DIR
    LOCAL_DIR="$(mktemp -d /tmp/github.XXXX)"
    local KEYFILE_TEMP
    KEYFILE_TEMP="$LOCAL_DIR/keyfile.txt"
    GITHUB_KEYFILE="${KEYFILE_TEMP}"
    gsutil -q cp "gs://${CB_TEST_GITHUB_TOKEN_FILE_PATH}" "${KEYFILE_TEMP}"
  fi
}

function create_manifest_check_consistency() {
  local MANIFEST_FILE=$1

  local ISTIO_REPO_SHA
  local PROXY_REPO_SHA
  local CNI_REPO_SHA
  local API_REPO_SHA
  ISTIO_REPO_SHA=$(git rev-parse HEAD)
  CNI_REPO_SHA=$(grep CNI_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
  PROXY_REPO_SHA=$(grep PROXY_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
  API_REPO_SHA=$(grep istio.io.api -A 100 < Gopkg.lock | grep revision -m 1 | cut -f 2 -d '"')

  if [ -z "${ISTIO_REPO_SHA}" ] || [ -z "${API_REPO_SHA}" ] || [ -z "${PROXY_REPO_SHA}" ] || [ -z "${CNI_REPO_SHA}" ] ; then
    echo "ISTIO_REPO_SHA:$ISTIO_REPO_SHA API_REPO_SHA:$API_REPO_SHA PROXY_REPO_SHA:$PROXY_REPO_SHA CNI_REPO_SHA:$CNI_REPO_SHA some shas not found"
    exit 8
  fi
cat << EOF > "${MANIFEST_FILE}"
istio ${ISTIO_REPO_SHA}
proxy ${PROXY_REPO_SHA}
api ${API_REPO_SHA}
cni ${CNI_REPO_SHA}
tools ${TOOLS_HEAD_SHA}
EOF


  if [[ "${CB_VERIFY_CONSISTENCY}" == "true" ]]; then
     pushd ../proxy
       PROXY_API_SHA=$(grep ISTIO_API istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
     popd
     if [ "$PROXY_API_SHA" != "$API_REPO_SHA" ]; then
       echo "inconsistent shas PROXY_API_SHA $PROXY_API_SHA !=   $API_REPO_SHA   API_REPO_SHA" 1>&2
       exit 17
     fi
  fi
}
