#!/bin/bash

# Copyright Istio Authors

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

function write_patch_file() {
    if [ -z "${ARTIFACTS}" ]; then
      return 0
    fi

    PATCH_NAME="check-clean-repo-diff.patch"
    PATCH_OUT="${ARTIFACTS}/${PATCH_NAME}"
    git diff > "${PATCH_OUT}"

    [ -n "${JOB_NAME}" ] && [ -n "${BUILD_ID}" ]
    IN_PROW="$?"

    # Don't persist large diffs (30M+) on CI
    LARGE_FILE="$(find "${ARTIFACTS}" -name "${PATCH_NAME}" -type 'f' -size +30M)"
    if [ "${IN_PROW}" -eq 0 ] && [ -n "${LARGE_FILE}" ]; then
      rm "${PATCH_OUT}"
      echo "WARNING: patch file was too large to persist ($(du -h "${PATCH_OUT}"))"
      return 0
    fi
    echo "You can also try applying the patch file from the build artifacts."
}

if [[ -n $(git status --porcelain) ]]; then
  git status
  git diff
  echo "ERROR: Some files need to be updated, please run 'make gen' and include any changed files in your PR"
  write_patch_file
  exit 1
fi
