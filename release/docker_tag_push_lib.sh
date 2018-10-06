#!/bin/bash
# Copyright 2018 Istio Authors. All Rights Reserved.
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

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "error this script should be sourced"
  exit 4
fi

if [[ -z "$(command -v docker)" ]]; then
    echo "Could not find 'docker' in path"
    exit 1
fi

function add_license_to_tar_images() {
  local HUB
  HUB="$1"
  local TAG
  TAG="$2"
  local OUT_PATH
  OUT_PATH="$3"

  for TAR_PATH in "${OUT_PATH}"/docker/*.tar.gz; do
    BASE_NAME=$(basename "$TAR_PATH")
    TAR_NAME="${BASE_NAME%.*}"
    IMAGE_NAME="${TAR_NAME%.*}"

    # if no docker/ directory or directory has no tar files
    if [[ "${IMAGE_NAME}" == "*" ]]; then
      break
    fi
    docker load -i "${TAR_PATH}"
    echo "FROM istio/${IMAGE_NAME}:${TAG}
COPY LICENSES.txt /" > Dockerfile
    docker build -t              "${HUB}/${IMAGE_NAME}:${TAG}" .
    # Include the license text in the tarball as well (overwrite old $TAR_PATH).
    docker save -o "${TAR_PATH}" "${HUB}/${IMAGE_NAME}:${TAG}"
  done
}

function docker_tag_images() {
  local DST_HUB
  DST_HUB="$1"
  local DST_TAG
  DST_TAG="$2"
  local OUT_PATH
  OUT_PATH="$3"

  for TAR_PATH in "${OUT_PATH}"/docker/*.tar.gz; do
    BASE_NAME=$(basename "$TAR_PATH")
    TAR_NAME="${BASE_NAME%.*}"
    IMAGE_NAME="${TAR_NAME%.*}"

    # if no docker/ directory or directory has no tar files
    if [[ "${IMAGE_NAME}" == "*" ]]; then
      break
    fi
    docker load -i "${TAR_PATH}"
    DOCKER_OUT=$(docker load -i "${TAR_PATH}")
    SRC_HUB=$(echo "$DOCKER_OUT" | cut -f 2 -d : | xargs dirname)
    SRC_TAG=$(echo "$DOCKER_OUT" | cut -f 3 -d :)

    docker tag     "${SRC_HUB}/${IMAGE_NAME}:${SRC_TAG}" \
                   "${DST_HUB}/${IMAGE_NAME}:${DST_TAG}"
  done
}

function add_docker_creds() {
  local PUSH_HUB
  PUSH_HUB="$1"

  local ADD_DOCKER_KEY
  ADD_DOCKER_KEY="true"
  if [[ "${ADD_DOCKER_KEY}" != "true" ]]; then
     return
  fi

  if [[ "${PUSH_HUB}" == "docker.io/istio" ]]; then
    echo "using istio cred for docker"
    gsutil -q cp gs://istio-secrets/dockerhub_config.json.enc "$HOME/.docker/config.json.enc"
    gcloud kms decrypt \
       --ciphertext-file="$HOME/.docker/config.json.enc" \
       --plaintext-file="$HOME/.docker/config.json" \
       --location=global \
       --keyring=${KEYRING} \
       --key=${KEY}
    return
  fi

  if [[ "${PUSH_HUB}" == "docker.io/testistio" ]]; then
    gsutil cp gs://istio-secrets/docker.test.json $HOME/.docker/config.json
  fi

  if [[ "${PUSH_HUB}" == gcr.io* ]]; then
    gcloud auth configure-docker -q
  fi
}

function docker_push_images() {
  local DST_HUB
  DST_HUB="$1"
  local DST_TAG
  DST_TAG="$2"
  local OUT_PATH
  OUT_PATH="$3"
  echo "pushing to ${DST_HUB}/image:${DST_TAG}"
  add_docker_creds "${DST_HUB}"

  for TAR_PATH in "${OUT_PATH}"/docker/*.tar.gz; do
    BASE_NAME=$(basename "$TAR_PATH")
    TAR_NAME="${BASE_NAME%.*}"
    IMAGE_NAME="${TAR_NAME%.*}"

    # if no docker/ directory or directory has no tar files
    if [[ "${IMAGE_NAME}" == "*" ]]; then
      break
    fi
    docker load -i "${TAR_PATH}"
    docker push    "${DST_HUB}/${IMAGE_NAME}:${DST_TAG}"
  done
}
