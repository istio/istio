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

# This command takes a snapshot of the charts and profiles which are used to generate golden files.

# No unset vars, print commands as they're executed, and exit on any non-zero
# return code
set -u
set -x
set -e

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR="${SCRIPTPATH}/../.."

MANIFESTS_DIR="${ROOTDIR}/manifests"
CHARTS_SNAPSHOT="${ROOTDIR}/operator/cmd/mesh/testdata/manifest-generate/data-snapshot"

# Clean up existing files
rm -Rf "${CHARTS_SNAPSHOT:?}/"

cp -Rf "${MANIFESTS_DIR}" "${CHARTS_SNAPSHOT}"/
