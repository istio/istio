#!/bin/bash

# Copyright 2019 Istio Authors
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

set -x
set -e

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

function update_branch() {
    local FROM="${1}"
    local TO="${2}"

    if [ "${FROM}" != "${TO}" ]; then
        echo "Updating version for branch ${TO}..."
        # Update version string in docs.
        sed -i "s|blob/${FROM}|blob/${TO}|g" "${ROOT}"/ARCHITECTURE.md
        sed -i "s|blob/${FROM}|blob/${TO}|g" "${ROOT}"/README.md
        # Update tag for buildin profiles.
        find "${ROOT}"/data/profiles -type f -exec sed -i "s/tag: ${FROM}-latest-daily/tag: ${TO}-latest-daily/g" {} \;
        # Update tag for testdata.
        find "${ROOT}"/cmd/mesh/testdata -type f -exec sed -i "s/tag: ${FROM}-latest-daily/tag: ${TO}-latest-daily/g" {} \;
        find "${ROOT}"/pkg/values/testdata -type f -exec sed -i "s/tag: ${FROM}-latest-daily/tag: ${TO}-latest-daily/g" {} \;
        # Update operator version.
        find "${ROOT}"/version -type f -exec sed -r "s/[0-9]+\.[0-9]+\.[0-9]+/${OPERATOR_VERSION}/g" {} \;
    fi
}

FROM_BRANCH=${FROM_BRANCH:-master}
CURRENT_BRANCH="$(git rev-parse --abbrev-ref HEAD)"

SHORT_VERSION=${CURRENT_BRANCH//release-/}
[[ ${SHORT_VERSION} =~ ^[0-9]+\.[0-9]+ ]] && SHORT_VERSION=${BASH_REMATCH[0]}
PATCH_VERSION=${PATCH_VERSION:-0}
OPERATOR_VERSION="${SHORT_VERSION}.${PATCH_VERSION}"

update_branch "${FROM_BRANCH}" "${CURRENT_BRANCH}"
