#!/bin/bash

# Copyright Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

if BUILD_GIT_REVISION=$(git rev-parse HEAD 2> /dev/null); then
    if ! git diff-index --quiet HEAD; then
        BUILD_GIT_REVISION=${BUILD_GIT_REVISION}"-dirty"
    fi
else
    BUILD_GIT_REVISION=unknown
fi

# Check for local changes
if git diff-index --quiet HEAD --; then
  tree_status="Clean"
else
  tree_status="Modified"
fi

# XXX This needs to be updated to accommodate tags added after building, rather than prior to builds
RELEASE_TAG=$(git describe --match '[0-9]*\.[0-9]*\.[0-9]*' --exact-match 2> /dev/null || echo "")

# security wanted VERSION='unknown'
VERSION="${BUILD_GIT_REVISION}"
if [[ -n "${RELEASE_TAG}" ]]; then
  VERSION="${RELEASE_TAG}"
elif [[ -n ${ISTIO_VERSION} ]]; then
  VERSION="${ISTIO_VERSION}"
fi

DOCKER_HUB="docker.io/istio"
if [[ -n ${ISTIO_DOCKER_HUB} ]]; then
  DOCKER_HUB="${ISTIO_DOCKER_HUB}"
fi

# used by common/scripts/gobuild.sh
echo "istio.io/pkg/version.buildVersion=${VERSION}"
echo "istio.io/pkg/version.buildGitRevision=${BUILD_GIT_REVISION}"
echo "istio.io/pkg/version.buildUser=$(whoami)"
echo "istio.io/pkg/version.buildHost=$(hostname -f)"
echo "istio.io/pkg/version.buildDockerHub=${DOCKER_HUB}"
echo "istio.io/pkg/version.buildStatus=${tree_status}"
