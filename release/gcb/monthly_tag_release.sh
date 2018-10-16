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

source "/workspace/gcb_env.sh"

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )

KEYFILE_DECRYPT="true"
UPLOAD_DIR="$(mktemp -d /tmp/release.XXXX)"
KEYFILE_ENC="${UPLOAD_DIR}/keyfile.enc"
KEYFILE_TEMP="${UPLOAD_DIR}/keyfile.txt"

KEYRING="Secrets"
KEY="DockerHub"

function usage() {
  echo "$0
    -d         disables decryption of file"
  exit 1
}

while getopts d arg ; do
  case "${arg}" in
    d) KEYFILE_DECRYPT="false";;
    *) usage;;
  esac
done

[[ -z "${CB_VERSION}" ]] && usage
[[ -z "${CB_GCS_GITHUB_PATH}" ]] && usage
[[ -z "${CB_GITHUB_ORG}" ]] && usage

gsutil -m cp "gs://${CB_GCS_GITHUB_PATH}" "${KEYFILE_TEMP}"
KEYFILE="${KEYFILE_TEMP}"

# decrypt file, if requested
if [[ "${KEYFILE_DECRYPT}" == "true" ]]; then

  cp "${KEYFILE}" "${KEYFILE_ENC}"
  gcloud kms decrypt \
       --ciphertext-file="$KEYFILE_ENC" \
       --plaintext-file="$KEYFILE_TEMP" \
       --location=global \
       --keyring=${KEYRING} \
       --key=${KEY}

  KEYFILE="${KEYFILE_TEMP}"
fi

USER_NAME="IstioReleaserBot"
USER_EMAIL="istio_releaser_bot@example.com"

echo "Beginning tag of github"
"${SCRIPTPATH}/create_tag_reference.sh" -k "$KEYFILE" -v "${CB_VERSION}" -o "${CB_GITHUB_ORG}" \
           -e "${USER_EMAIL}" -n "${USER_NAME}" -b "/workspace/manifest.txt"
echo "Completed tag of github"

