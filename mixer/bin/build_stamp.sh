#!/bin/bash

# Copyright 2017 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# This must be kept in sync with //pkg/version/version.go

# Setup build ID based on date and short SHA of latest commit.
SHORT_SHA="$(git rev-parse --short HEAD)"
echo "buildID $(date +%F)-${SHORT_SHA}"

# Check for local changes
git diff-index --quiet HEAD --
if [[ $? == 0 ]];
then
    tree_status="Clean"
else
    tree_status="Modified"
fi
echo "buildStatus ${tree_status}"

# Check for version informatioqn
RELEASE_TAG=$(git describe --match '[0-9]*\.[0-9]*\.[0-9]*' 2> /dev/null || echo "")
if [[ -n "${RELEASE_TAG}" ]]; then
  VERSION="${RELEASE_TAG}"
elif [[ -n ${ISTIO_VERSION} ]]; then
  VERSION="${ISTIO_VERSION}-${SHORT_SHA}"
fi

if [[ -n ${VERSION} ]]; then
    echo "buildVersion ${VERSION}"
fi
