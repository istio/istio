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
HELM_HUB=${HELM_HUB:-gcr.io/istio-testing/charts}
GCS_BUCKET=${GCS_BUCKET:-istio-build/dev}

# Enable emulation required for cross compiling a few images (VMs)
docker run --rm --privileged "${DOCKER_HUB}/qemu-user-static" --reset -p yes
export ISTIO_DOCKER_QEMU=true

# Use a pinned version in case breaking changes are needed
BUILDER_SHA=2cb0e86eb7e189578e5149980b3f42b0889dfad6

# Reference to the next minor version of Istio
# This will create a version like 1.4-alpha.sha
NEXT_VERSION=$(cat "${ROOT}/VERSION")
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
  client-go:
    git: https://github.com/istio/client-go
    auto: modules
  test-infra:
    git: https://github.com/istio/test-infra
    branch: master
  tools:
    git: https://github.com/istio/tools
    branch: master
  release-builder:
    git: https://github.com/istio/release-builder
    sha: ${BUILDER_SHA}
  ztunnel:
    git: https://github.com/istio/ztunnel
    auto: deps
architectures: [linux/amd64, linux/arm64]
EOD
)}
dashboards:
  istio-mesh-dashboard: 7639
  istio-performance-dashboard: 11829
  istio-service-dashboard: 7636
  istio-workload-dashboard: 7630
  pilot-dashboard: 7645
  istio-extension-dashboard: 13277
  ztunnel-dashboard: 21306
${PROXY_OVERRIDE:-}
EOF
)

# "Temporary" hacks
export PATH=${GOPATH}/bin:${PATH}

go install "istio.io/release-builder@${BUILDER_SHA}"

release-builder build --manifest <(echo "${MANIFEST}")

release-builder validate --release "${WORK_DIR}/out"

if [[ -z "${DRY_RUN:-}" ]]; then
  release-builder publish --release "${WORK_DIR}/out" \
    --gcsbucket "${GCS_BUCKET}" --gcsaliases "${TAG},${NEXT_VERSION}-dev,latest" \
    --dockerhub "${DOCKER_HUB}" --helmhub "${HELM_HUB}" --dockertags "${TAG},${VERSION},${NEXT_VERSION}-dev,latest"
fi
