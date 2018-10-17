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

# this function sets variable TEST_INFRA_DIR and githubctl
function githubctl_setup() {
    git clone https://github.com/istio/test-infra.git -b master --depth 1
    TEST_INFRA_DIR="${PWD}/test-infra"
    pushd "${TEST_INFRA_DIR}" || exit 1
     if [[ -f "/workspace/githubctl" ]]; then
       githubctl="/workspace/githubctl"
     else
       bazel build //toolbox/githubctl
       githubctl="${TEST_INFRA_DIR}/bazel-bin/toolbox/githubctl/linux_amd64_stripped/githubctl" 
    fi
    popd || exit 1

   export TEST_INFRA_DIR
   export githubctl
}

#sets GITHUB_KEYFILE to github auth file
function github_keys() {
  local LOCAL_DIR
  LOCAL_DIR="$(mktemp -d /tmp/github.XXXX)"
  local KEYFILE_ENC
  KEYFILE_ENC="$LOCAL_DIR/keyfile.enc"
  local KEYFILE_TEMP
  KEYFILE_TEMP="$LOCAL_DIR/keyfile.txt"
  local KEYRING
  KEYRING="Secrets"
  local KEY
  KEY="DockerHub"

 # decrypt file, if requested
  gsutil -q cp gs://istio-secrets/github.txt.enc "${KEYFILE_ENC}"
  gcloud kms decrypt \
       --ciphertext-file="$KEYFILE_ENC" \
       --plaintext-file="$KEYFILE_TEMP" \
       --location=global \
       --keyring="${KEYRING}" \
       --key="${KEY}"

  GITHUB_KEYFILE="${KEYFILE_TEMP}"
  export GITHUB_KEYFILE
}
