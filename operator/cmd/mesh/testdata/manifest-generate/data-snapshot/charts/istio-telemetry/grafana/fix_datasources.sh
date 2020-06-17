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

set -e

THIS_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
UX=$(uname)

for db in "${THIS_DIR}"/dashboards/*.json; do
    if [[ ${UX} == "Darwin" ]]; then
        # shellcheck disable=SC2016
        sed -i '' 's/${DS_PROMETHEUS}/Prometheus/g' "$db"
    else
        # shellcheck disable=SC2016
        sed -i 's/${DS_PROMETHEUS}/Prometheus/g' "$db"
    fi
done
