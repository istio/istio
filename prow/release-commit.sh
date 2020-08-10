#!/bin/bash

# Copyright Istio Authors
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

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

set -eux

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"

setup_gcloud_credentials

# Old prow image does not set this, so needed explicitly here as this is not called through make
export GO111MODULE=on

DOCKER_HUB=${DOCKER_HUB:-gcr.io/istio-testing}
GCS_BUCKET=${GCS_BUCKET:-istio-build/dev}

# Use a pinned version in case breaking changes are needed
BUILDER_SHA=dedd0ac061a7b043f845cc26fbf2cb92e1cd1e18

# Reference to the next minor version of Istio
# This will create a version like 1.4-alpha.sha
NEXT_VERSION=1.7
TAG=$(git rev-parse HEAD)
VERSION="${NEXT_VERSION}-alpha.${TAG}"

# In CI we want to store the outputs to artifacts, which will preserve the build
# If not specified, we can just create a temporary directory
WORK_DIR="$(mktemp -d)/build"
mkdir -p "${WORK_DIR}"

MANIFEST=$(cat <<EOF
version: ${VERSION}
docker: ${DOCKER_HUB}
directory: ${WORK_DIR}
ignoreVulnerability: true
dependencies:
${DEPENDENCIES:-$(cat <<EOD
  istio:
    localpath: ${ROOT}
  api:
    git: https://github.com/istio/api
    auto: modules
  proxy:
    git: https://github.com/istio/proxy
    auto: deps
  pkg:
    git: https://github.com/istio/pkg
    auto: modules
  client-go:
    git: https://github.com/istio/client-go
    branch: master
  gogo-genproto:
    git: https://github.com/istio/gogo-genproto
    branch: master
  test-infra:
    git: https://github.com/istio/test-infra
    branch: master
  tools:
    git: https://github.com/istio/tools
    branch: master
  envoy:
    git: https://github.com/istio/envoy
    auto: proxy_workspace
EOD
)}
dashboards:
  istio-mesh-dashboard: 7639
  istio-performance-dashboard: 11829
  istio-service-dashboard: 7636
  istio-workload-dashboard: 7630
  mixer-dashboard: 7642
  pilot-dashboard: 7645
${PROXY_OVERRIDE:-}
EOF
)

# "Temporary" hacks
export PATH=${GOPATH}/bin:${PATH}

# cd to not impact go.mod
(cd /tmp; go get "istio.io/release-builder@${BUILDER_SHA}")

release-builder build --manifest <(echo "${MANIFEST}")

release-builder validate --release "${WORK_DIR}/out"

if [[ -z "${DRY_RUN:-}" ]]; then
  release-builder publish --release "${WORK_DIR}/out" \
    --gcsbucket "${GCS_BUCKET}" --gcsaliases "${NEXT_VERSION}-dev" \
    --dockerhub "${DOCKER_HUB}" --dockertags "${VERSION},${NEXT_VERSION}-dev"
fi
