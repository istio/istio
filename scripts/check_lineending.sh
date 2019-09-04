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

set -eu

TMP_FILE=$(mktemp)

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR=$(dirname "${SCRIPTPATH}")
IFS=""

find "${ROOTDIR}" -name "*.yaml" -or -name "*.yml" | while read -r f; do [ -z "$(tail -n 1 $f | tr -cd '\n' | tr '\n' 'v')" ] && echo "missing ending newline in $f" >> "${TMP_FILE}"; done || true

if [[ -s "${TMP_FILE}" ]];
then
    cat "${TMP_FILE}"
    exit 1
fi
