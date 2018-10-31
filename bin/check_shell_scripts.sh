#!/bin/sh

# Copyright 2018 Istio Authors
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

# Runs shellcheck on all shell scripts in the istio repository.

if ! command -v shellcheck > /dev/null; then
    echo 'error: ShellCheck is not installed'
    echo 'Visit https://github.com/koalaman/shellcheck#installing'
    exit 1
fi

BASE_DIR="$(cd "$(dirname "${0}")" && pwd -P)"
ISTIO_ROOT="$(cd "$(dirname "${BASE_DIR}")" && pwd -P)"

# All files ending in .sh.
SH_FILES=$( \
    find "${ISTIO_ROOT}" \
        -name '*.sh' -type f \
        -not -path '*/vendor/*' \
        -not -path '*/.git/*')
# All files not ending in .sh but starting with a shebang.
SHEBANG_FILES=$( \
    find "${ISTIO_ROOT}" \
        -not -name '*.sh' -type f \
        -not -path '*/vendor/*' \
        -not -path '*/.git/*' | \
        while read -r f; do
            head -n 1 "$f" | grep -q '^#!.*sh' && echo "$f";
        done)

# Set global rule exclusions with the "exclude" flag, separated by comma (e.g.
# "--exclude=SC1090,SC1091"). See https://github.com/koalaman/shellcheck/wiki
# for details on each code's corresponding rule.

echo "${SH_FILES}" "${SHEBANG_FILES}" \
    | xargs shellcheck
