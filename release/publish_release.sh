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

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ORG=""
REPO=""
KEYFILE=""
KEYFILE_GCS=""
KEYFILE_DECRYPT="false"
TOKEN=""
VERSION=""
PRODUCT="istio"
GCS_SOURCE=""
LOCAL_DIR="$(mktemp -d /tmp/release.${PRODUCT}.XXXX)"
KEYFILE_ENC=$LOCAL_DIR/keyfile.enc
KEYFILE_TEMP=$LOCAL_DIR/keyfile.txt
USER_EMAIL=""
USER_NAME=""
UPLOAD_DIR=$LOCAL_DIR

GCS_DEST=""

DO_GCS="true"
DO_GITHUB_TAG="true"
DO_GITHUB_REL="true"

KEYRING="Secrets"
KEY="DockerHub"

function usage() {
  echo "$0
Options for disabling types of publishing:
    -w         disables releasing to gcs
    -q         disables tagging on github
    -x         disables releasing to github
Options relevant to most features:
    -g <uri>   source of files on gcs (use this or -u)
    -u <dir>   source directory from which to upload artifacts (use this or -g)
    -v <ver>   version tag of release
Options specific to github (tag and/or release):
    -k <file>  file that contains github user token
    -l <gcs>   gcs path from which to get file with user token (alt to -k)
    -m         enables decryption of file
    -t <tok>   github user token to use in REST calls (use this or -k or -l)
    -o <name>  github org of repo
Options specific to github tag:
    -e <email> email of submitter
    -n <name>  name of submitter
Options specific to github release:
    -r <name>  github repo name
Options specific to publishing to gcs:
    -h <uri>   dest for files on gcs"
  exit 1
}

while getopts e:g:h:k:l:mn:o:qr:t:u:v:wx arg ; do
  case "${arg}" in
    e) USER_EMAIL="${OPTARG}";;
    g) GCS_SOURCE="${OPTARG}";;
    h) GCS_DEST="${OPTARG}";;
    k) KEYFILE="${OPTARG}";;
    l) KEYFILE_GCS="${OPTARG}";;
    m) KEYFILE_DECRYPT="true";;
    n) USER_NAME="${OPTARG}";;
    o) ORG="${OPTARG}";;
    q) DO_GITHUB_TAG="false";;
    r) REPO="${OPTARG}";;
    t) TOKEN="${OPTARG}";;
    u) UPLOAD_DIR="${OPTARG}";;
    v) VERSION="${OPTARG}";;
    w) DO_GCS="false";;
    x) DO_GITHUB_REL="false";;
    *) usage;;
  esac
done

[[ -z "${VERSION}" ]] && usage


if [[ "${DO_GCS}" == "false" && "${DO_GITHUB_TAG}" == "false" && "${DO_GITHUB_REL}" == "false" ]]; then
  echo "All operations supported by this script are disabled"
  usage
  exit 1
fi

# if GCS requested then copy it locally
if [[ -n "${KEYFILE_GCS}" ]]; then
  if [[ -n "${KEYFILE}" ]]; then
     echo "use only one of -k or -l"
     usage
     exit 1
  fi
  gsutil -m cp "gs://${KEYFILE_GCS}" "${KEYFILE_TEMP}"
  KEYFILE="${KEYFILE_TEMP}"
fi

# decrypt file, if requested
if [[ "${KEYFILE_DECRYPT}" == "true" ]]; then

  if [[ -z "${KEYFILE}" ]]; then
    echo "-m requires either -k or -l"
    usage
    exit 1
  fi

  cp "${KEYFILE}" "${KEYFILE_ENC}"
  gcloud kms decrypt \
       --ciphertext-file="$KEYFILE_ENC" \
       --plaintext-file="$KEYFILE_TEMP" \
       --location=global \
       --keyring=${KEYRING} \
       --key=${KEY}

  KEYFILE="${KEYFILE_TEMP}"
fi

# basic validation of inputs based on features used

# trim any trailing / for simplicity and consistency
UPLOAD_DIR=${UPLOAD_DIR%/}

if [[ "${DO_GITHUB_TAG}" == "true" ]]; then
  [[ -z "${TOKEN}" ]] && [[ -z "${KEYFILE}" ]] && usage
  [[ -z "${ORG}" ]] && usage
  # I tried using /user to automatically get name and email, but they're both null
  [[ -z "${USER_NAME}" ]] && usage
  [[ -z "${USER_EMAIL}" ]] && usage
  [[ -z "${UPLOAD_DIR}" ]] && usage
fi

if [[ "${DO_GITHUB_REL}" == "true" ]]; then
  [[ -z "${TOKEN}" ]] && [[ -z "${KEYFILE}" ]] && usage
  [[ -z "${ORG}" ]] && usage
  [[ -z "${REPO}" ]] && usage
  [[ -z "${UPLOAD_DIR}" ]] && usage
fi

if [[ "${DO_GCS}" == "true" ]]; then
  [[ -z "${GCS_DEST}" ]] && usage
  GCS_DEST=${GCS_DEST%/}
fi

# if GCS source dir provided then copy files to local location

if [[ -n "${GCS_SOURCE}" ]]; then

  GCS_SOURCE=${GCS_SOURCE%/}

  if [[ "${UPLOAD_DIR}" != "${LOCAL_DIR}" ]]; then
     echo "-u should not be used when -g is specified"
     usage
     exit 1
  fi

  echo "Downloading files from $GCS_SOURCE to $UPLOAD_DIR"

  if [[ "${DO_GCS}" == "true" ]]; then
    mkdir -p "${UPLOAD_DIR}/deb/"
    gsutil -m cp gs://"${GCS_SOURCE}"/deb/istio*.deb        "${UPLOAD_DIR}/deb/"
    gsutil -m cp gs://"${GCS_SOURCE}"/deb/istio*.deb.sha256 "${UPLOAD_DIR}/deb/"
  fi
  if [[ "${DO_GITHUB_TAG}" == "true" || "${DO_GITHUB_REL}" == "true" ]]; then
    # previously copied to the /output dir by istio_checkout_code.sh
    cp "/output/manifest.xml" "${UPLOAD_DIR}/"
  fi
  if [[ "${DO_GITHUB_REL}" == "true" ]]; then
    gsutil -m cp gs://"${GCS_SOURCE}"/istio-*.zip "${UPLOAD_DIR}/"
    gsutil -m cp gs://"${GCS_SOURCE}"/istio-*.gz  "${UPLOAD_DIR}/"
    gsutil -m cp gs://"${GCS_SOURCE}"/istio-*.gz.sha256  "${UPLOAD_DIR}/"
    gsutil -m cp gs://"${GCS_SOURCE}"/istio-*.zip.sha256 "${UPLOAD_DIR}/"
  fi
  echo "Finished downloading files from GCS source"
fi

# at this point everything we need is on the local filesystem

if [[ "${DO_GCS}" == "true" ]]; then
  echo "Copying to GCS destination ${GCS_DEST}"
  gsutil -m cp "${UPLOAD_DIR}"/deb/istio*.deb        "gs://${GCS_DEST}/deb/"
  gsutil -m cp "${UPLOAD_DIR}"/deb/istio*.deb.sha256 "gs://${GCS_DEST}/deb/"
  echo "Done copying to GCS destination"
fi

if [[ "${DO_GITHUB_TAG}" == "true" ]]; then
  echo "Beginning tag of github"
  if [[ -n "${KEYFILE}" ]]; then
    "${SCRIPTPATH}/create_tag_reference.sh" -k "$KEYFILE" -v "$VERSION" -o "${ORG}" \
           -e "${USER_EMAIL}" -n "${USER_NAME}" -b "${UPLOAD_DIR}/manifest.xml"
  else
    "${SCRIPTPATH}/create_tag_reference.sh" -t "$TOKEN" -v "$VERSION" -o "${ORG}" \
           -e "${USER_EMAIL}" -n "${USER_NAME}" -b "${UPLOAD_DIR}/manifest.xml"
  fi
  echo "Completed tag of github"
else
  echo "Skipping tag of github"
fi

if [[ "${DO_GITHUB_REL}" == "true" ]]; then

  SHA=$(grep "$ORG/$REPO" "${UPLOAD_DIR}/manifest.xml" | cut -f 6 -d \")

  echo "Beginning release to github using sha $SHA"
  if [[ -n "${KEYFILE}" ]]; then
    "${SCRIPTPATH}/create_github_release.sh" -o "$ORG" -r "$REPO" -k "$KEYFILE" \
           -v "$VERSION" -s "$SHA" -u "${UPLOAD_DIR}"
  else
    "${SCRIPTPATH}/create_github_release.sh" -o "$ORG" -r "$REPO" -t "$TOKEN" \
           -v "$VERSION" -s "$SHA" -u "${UPLOAD_DIR}"
  fi
  echo "Completed release to github"
else
  echo "Skipping release to github"
fi

rm -r -f "${LOCAL_DIR}"
