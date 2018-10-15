#!/bin/bash

# Copyright 2018 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.


# This script runs githubctl which sends a PR to github daily-releases
# to test the build

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

source "/workspace/gcb_env.sh"

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb_lib.sh
source "${SCRIPTPATH}/gcb_lib.sh"


function usage() {
  echo "$0
    uses CB_BRANCH CB_DOCKER_HUB CB_GCS_BUILD_PATH CB_VERSION"
  exit 1
}

[[ -z "${CB_BRANCH}"         ]] && usage
[[ -z "${CB_DOCKER_HUB}"     ]] && usage
[[ -z "${CB_GCS_BUILD_PATH}" ]] && usage
[[ -z "${CB_VERSION}"        ]] && usage

githubctl_setup
github_keys

# this setting is required by githubctl, which runs git commands
git config --global user.name "TestRunnerBot"	
git config --global user.email "testrunner@istio.io"

MANIFEST_FILE="/workspace/manifest.txt"
ISTIO_SHA=$(grep "istio" "$MANIFEST_FILE" | cut -f 2 -d " ")

"$githubctl" \
    --token_file="$GITHUB_KEYFILE" \
    --op=dailyRelQual \
    --base_branch="$CB_BRANCH" \
    --hub="$CB_DOCKER_HUB" \
    --gcs_path="$CB_GCS_BUILD_PATH" \
    --tag="$CB_VERSION" \
    --ref_sha="$ISTIO_SHA"

