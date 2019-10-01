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

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

set -eux

if [[ $(command -v gcloud &> /dev/null) ]]; then
  gcloud auth configure-docker -q
elif [[ $(command -v docker-credential-gcr &> /dev/null) ]]; then
  docker-credential-gcr configure-docker
else
  echo "No credential helpers found, push to docker may not function properly"
fi

DOCKER_HUB=${DOCKER_HUB:-gcr.io/istio-release}
GCS_BUCKET=${GCS_BUCKET:-istio-prerelease/dev}

# Reference to the next minor version of Istio
# This will create a version like 1.4-alpha.sha
NEXT_VERSION=1.4
TAG=$(git rev-parse HEAD)
VERSION="${NEXT_VERSION}-alpha.${TAG}"

# In CI we want to store the outputs to artifacts, which will preserve the build
# If not specified, we can just create a temporary directory
WORK_DIR="$(mktemp -d)/build"
mkdir -p "${WORK_DIR}"

MANIFEST=$(cat <<EOF
version: ${VERSION}
docker: docker.io/istio
directory: ${WORK_DIR}
dependencies:
  - org: istio
    repo: istio
    localpath: ${ROOT}
  - org: istio
    repo: cni
    auto: deps
EOF
)

# "Temporary" hacks
export PATH=${GOPATH}/bin:${PATH}

# cd to not impact go.mod
(cd /tmp; go get istio.io/release-builder)

release-builder build --manifest <(echo "${MANIFEST}")

if [[ -z "${DRY_RUN:-}" ]]; then
  release-builder publish --release "${WORK_DIR}/out" --gcsbucket "${GCS_BUCKET}" --dockerhub "${DOCKER_HUB}" --dockertags "${TAG},${NEXT_VERSION}-dev,latest"
fi