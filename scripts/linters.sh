#!/bin/bash

# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run the
# scripts/updatecommonfiles.sh script.

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

set -e

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR=$(dirname "${SCRIPTPATH}")
cd "${ROOTDIR}"

if [[ "$1" == "--fix" ]]
then
    FIX="--fix"
fi

echo 'Linting Go code...'
scripts/run_golangci.sh
echo 'Go code OK'

echo 'Checking licenses...'
scripts/check_license.sh
echo 'Licenses OK'

# run any specialized per-repo linters
if test -f "${ROOTDIR}/repolinters.sh"
then
    source "${ROOTDIR}/repolinters.sh"
fi
