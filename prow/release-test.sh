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

# Runs after a submit is merged to master:
# - run the unit tests, in local environment
# - push the docker images to gcr.io

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

set -eux

# Reference to the next minor version of Istio
# This will create a version like 1.4.0-alpha.20191001
NEXT_VERSION=1.4.0
DATE=$(date '+%Y%m%d')
VERSION="${NEXT_VERSION}-alpha.${DATE}"

# In CI we want to store the outputs to artifacts, which will preserve the build
# If not specified, we can just create a temporary directory
WORK_DIR="$(mktemp -d)/build"
mkdir -p "${WORK_DIR}"

# TODO pin version of CNI

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
    branch: master
EOF
)

echo "${MANIFEST}"

# tmp hacks
export PATH=${GOPATH}/bin:${PATH}
export GOSUMDB=sum.golang.org

# cd to not impact go.mod
(cd /tmp; go get istio.io/release-builder)

release-builder build --manifest <(echo "${MANIFEST}")
release-builder publish --release "${WORK_DIR}/out" --gcsbucket istio-release-pipeline-data/commit --dockerhub gcr.io/istio-prerelease-testing
