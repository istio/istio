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

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# This is a script to download release artifact from monthly or daily release
# location and test istioctl.
#
# This is currently triggered by https://github.com/istio-releases/daily-release
# for release qualification.
#
# Expects ISTIO_REL_URL, HUB, and TAG as inputs.


# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"

function test_istioctl_version() {
  local istioctl_bin=${1}
  local expected_hub=${2}
  local expected_tag=${3}

  hub=$(${istioctl_bin} version --remote=false --short=false | grep -oP 'Hub:"\K.*?(?=")')
  tag=$(${istioctl_bin} version --remote=false --short=false | grep -oP '{Version:"\K.*?(?=")')
  [ "${hub}" == "${expected_hub}" ]
  [ "${tag}" == "${expected_tag}" ]
}

function test_helm_files() {
  local istio_path=${1}
  local expected_hub=${2}
  local expected_tag=${3}

  hub=$(grep hub: "${istio_path}/install/kubernetes/helm/istio/values.yaml" | head -n 1 | cut -c 8-)
  tag=$(grep tag: "${istio_path}/install/kubernetes/helm/istio/values.yaml" | head -n 1 | cut -c 8-)
  [ "${hub}" == "${expected_hub}" ]
  [ "${tag}" == "${expected_tag}" ]
}


# Assert HUB and TAG are matching from all istioctl binaries.

download_untar_istio_release "${ISTIO_REL_URL}" "${TAG}"
test_istioctl_version "istio-${TAG}/bin/istioctl" "${HUB}" "${TAG}"
test_helm_files "istio-${TAG}" "${HUB}" "${TAG}"

