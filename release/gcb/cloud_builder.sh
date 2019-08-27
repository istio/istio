#!/bin/bash
# Copyright 2017 Istio Authors. All Rights Reserved.
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
#
################################################################################

set -o errexit
set -o nounset
set -o pipefail
set -x

# This file is primarily used by cloud builder to make
# an end-to-end built of istio.  It runs this script to place the
# build artifacts in a specified output directory, then runs
# create_release_archives.sh (via make istio-archive) to add tar files to the directory
# (based solely on the contents of that directory), and then
# uses store_artifacts.sh to store the build on GCS and docker hub
# in addition it saves the source code

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/gcb_lib.sh
source "${SCRIPTPATH}/gcb_lib.sh"

# directory that has the artifacts, hardcoded since the volume name in cloud_builder.json
# need to be the same also there is no value in making this configurable
OUTPUT_PATH="/output"

ROOT="$PWD"
GOPATH="$(cd "$ROOT/../../.." && pwd)"
export GOPATH
echo gopath is "$GOPATH"
# this is needed for istioctl and other parts of build to get the version info
export ISTIO_VERSION="${CB_VERSION}"

# build docker tar images
REL_DOCKER_HUB=docker.io/istio

make_istio "${OUTPUT_PATH}" "${CB_ISTIOCTL_DOCKER_HUB}" "${REL_DOCKER_HUB}" "${CB_VERSION}" "${CB_BRANCH}"
cp "/workspace/manifest.txt" "${OUTPUT_PATH}"
