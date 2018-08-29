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
GCR_DEST=""
DOCKER_DEST=""

DO_GCS="true"
DO_GITHUB_TAG="true"
DO_GITHUB_REL="true"
DO_GCRHUB="true"
ADD_DOCKER_KEY="false"
DO_DOCKERHUB="true"

KEYRING="Secrets"
KEY="DockerHub"

function usage() {
  echo "$0
Options for disabling types of publishing:
    -w         disables releasing to gcs
    -q         disables tagging on github
    -x         disables releasing to github
    -y         disables releasing to gcr
    -z         disables releasing to docker hub
Options relevant to most features:
    -g <uri>   source of files on gcs (use this or -u)
    -u <dir>   source directory from which to upload artifacts (use this or -g)
    -v <ver>   version tag of release
Options specific to docker hub:
    -c         use istio cred for docker (for cloud builder) (optional)
    -d         docker hub uri (Providing the string \"<none>\" here has the same affect as -z)
Options specific to gcr:
    -i <uri>   dest for images on gcr
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

while getopts cd:e:g:h:i:k:l:mn:o:qr:t:u:v:wxyz arg ; do
  case "${arg}" in
    c) ADD_DOCKER_KEY="true";;
    d) DOCKER_DEST="${OPTARG}";;
    e) USER_EMAIL="${OPTARG}";;
    g) GCS_SOURCE="${OPTARG}";;
    h) GCS_DEST="${OPTARG}";;
    i) GCR_DEST="${OPTARG}";;
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
    y) DO_GCRHUB="false";;
    z) DO_DOCKERHUB="false";;
    *) usage;;
  esac
done

[[ -z "${VERSION}" ]] && usage

if [[ "${DOCKER_DEST}" == "<none>" || -z "${DOCKER_DEST}" ]]; then
  echo "NOTE: An empty string was used for docker hub setting so docker push has been disabled"
  DO_DOCKERHUB="false"
fi

if [[ "${DO_GCS}" == "false" && "${DO_GITHUB_TAG}" == "false" && "${DO_GITHUB_REL}" == "false" && \
      "${DO_GCRHUB}" == "false" && "${DO_DOCKERHUB}" == "false" ]]; then
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

if [[ "${DO_GCRHUB}" == "true" ]]; then
  [[ -z "${GCR_DEST}" ]] && usage
  GCR_DEST=gcr.io/${GCR_DEST%/}
fi

if [[ "${DO_DOCKERHUB}" == "true" ]]; then
    DOCKER_DEST=docker.io/${DOCKER_DEST%/}
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
    gsutil -m cp gs://"${GCS_SOURCE}"/deb/istio*.deb "${UPLOAD_DIR}/deb/"
  fi
  if [[ "${DO_GITHUB_TAG}" == "true" || "${DO_GITHUB_REL}" == "true" ]]; then
    gsutil -m cp "gs://${GCS_SOURCE}/manifest.xml" "${UPLOAD_DIR}/"
  fi
  if [[ "${DO_GITHUB_REL}" == "true" ]]; then
    gsutil -m cp gs://"${GCS_SOURCE}"/docker.io/istio-*.zip "${UPLOAD_DIR}/"
    gsutil -m cp gs://"${GCS_SOURCE}"/docker.io/istio-*.gz  "${UPLOAD_DIR}/"
  fi
  if [[ "${DO_GCRHUB}" == "true" || "${DO_DOCKERHUB}" == "true" ]]; then
    mkdir -p "${UPLOAD_DIR}/docker/"
    gsutil -m cp gs://"${GCS_SOURCE}"/docker/*.tar.gz  "${UPLOAD_DIR}/docker/"
  fi
  echo "Finished downloading files from GCS source"
fi

# at this point everything we need is on the local filesystem

if [[ "${DO_GCS}" == "true" ]]; then
  echo "Copying to GCS destination ${GCS_DEST}"
  gsutil -m cp "${UPLOAD_DIR}"/deb/istio*.deb "gs://${GCS_DEST}/deb/"
# gsutil -m cp "${UPLOAD_DIR}/SHA256SUMS"     "gs://${GCS_DEST}/"
  echo "Done copying to GCS destination"
fi

if [[ "${DO_DOCKERHUB}" == "true" || "${DO_GCRHUB}" == "true" ]]; then
  if [[ -z "$(command -v docker)" ]]; then
    echo "Could not find 'docker' in path"
    exit 1
  fi

  if [[ "${DO_DOCKERHUB}" == "true" && "${ADD_DOCKER_KEY}" == "true" ]]; then
    echo "using istio cred for docker"
    gsutil -q cp gs://istio-secrets/dockerhub_config.json.enc "$HOME/.docker/config.json.enc"
    gcloud kms decrypt \
       --ciphertext-file="$HOME/.docker/config.json.enc" \
       --plaintext-file="$HOME/.docker/config.json" \
       --location=global \
       --keyring=${KEYRING} \
       --key=${KEY}
  fi

  echo "pushing images to docker and/or gcr"
  for TAR_PATH in "${UPLOAD_DIR}"/docker/*.tar.gz; do
    TAR_NAME=$(basename "$TAR_PATH")
    IMAGE_NAME="${TAR_NAME%.tar.gz}"

    # if no docker/ directory or directory has no tar files
    if [[ "${IMAGE_NAME}" == "*" ]]; then
      echo "No image tar files were found in docker/"
      exit 1
    fi
    DOCKER_OUT=$(docker load -i "${TAR_PATH}")
    DOCKER_SRC=$(echo "$DOCKER_OUT" | cut -f 2 -d : | xargs dirname)
    echo DOCKER_SRC is "$DOCKER_SRC"

    if [[ "${DO_DOCKERHUB}" == "true" ]]; then
      docker tag "${DOCKER_SRC}/${IMAGE_NAME}:${VERSION}" "${DOCKER_DEST}/${IMAGE_NAME}:${VERSION}"
      docker push                                         "${DOCKER_DEST}/${IMAGE_NAME}:${VERSION}"
    fi
    if [[ "${DO_GCRHUB}" == "true" ]]; then
      gcloud auth configure-docker -q
      docker tag "${DOCKER_SRC}/${IMAGE_NAME}:${VERSION}"    "${GCR_DEST}/${IMAGE_NAME}:${VERSION}"
      docker push                                            "${GCR_DEST}/${IMAGE_NAME}:${VERSION}"
    fi
  done

  echo "Finished pushing images to docker and/or gcr"
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
