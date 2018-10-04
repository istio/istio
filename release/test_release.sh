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

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb_lib.sh
source "${SCRIPTPATH}/gcb_lib.sh"


BRANCH=""
DOCKER_HUB=""
GCS_BUILD_PATH=""
VERSION=""

function usage() {
  echo "$0
    -b <name>       branch                          (required)
    -d <docker hub> full path of docker hub         (required)
    -g <uri>        the build path of the artifacts (required)
    -v <version>    version of the build            (required)
    -s <sha>        green sha                       (required)"
  exit 1
}

while getopts b:d:g:v: arg ; do
  case "${arg}" in
    b) BRANCH="${OPTARG}";;
    d) DOCKER_HUB="${OPTARG}";;
    g) GCS_BUILD_PATH="${OPTARG}";;
    v) VERSION="${OPTARG}";;
    s) SHA="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${BRANCH}"         ]] && usage
[[ -z "${DOCKER_HUB}"     ]] && usage
[[ -z "${GCS_BUILD_PATH}" ]] && usage
[[ -z "${VERSION}"        ]] && usage
[[ -z "${SHA}"            ]] && usage

githubctl_setup
github_keys

# this setting is required by githubctl, which runs git commands
git config --global user.name "TestRunnerBot"	
git config --global user.email "testrunner@istio.io"

MANIFEST_FILE="/output/manifest.xml"
ISTIO_SHA=$(grep "istio/istio" "$MANIFEST_FILE" | cut -f 6 -d \")

"$githubctl" \
    --token_file="$GITHUB_KEYFILE" \
    --op=dailyRelQual \
    --base_branch="$BRANCH" \
    --hub="$DOCKER_HUB" \
    --gcs_path="$GCS_BUILD_PATH" \
    --tag="$VERSION" \
    --ref_sha="$ISTIO_SHA"

