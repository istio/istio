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

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/gcb_lib.sh
source "${SCRIPTPATH}/gcb_lib.sh"


function usage() {
  echo "$0
       uses CB_VERSION CB_GCS_GITHUB_TOKEN_FILE_PATH CB_GITHUB_ORG"
  exit 1
}

[[ -z "${CB_VERSION}" ]] && usage
[[ -z "${CB_GCS_GITHUB_TOKEN_FILE_PATH}" ]] && usage
[[ -z "${CB_GITHUB_ORG}" ]] && usage

# github keys uses CB_GCS_GITHUB_TOKEN_FILE_PATH to find the github key file, decrypts if needed
# and sets GITHUB_KEYFILE
github_keys
[[ -z "${GITHUB_KEYFILE}" ]] && usage

USER_NAME="IstioReleaserBot"
USER_EMAIL="istio_releaser_bot@example.com"

echo "Beginning tag of github"
"${SCRIPTPATH}/create_tag_reference.sh" -k "${GITHUB_KEYFILE}" -v "${CB_VERSION}" -o "${CB_GITHUB_ORG}" \
           -e "${USER_EMAIL}" -n "${USER_NAME}" -b "/workspace/manifest.txt"
echo "Completed tag of github"

