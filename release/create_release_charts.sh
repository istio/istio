#!/usr/bin/env bash
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
set -o pipefail

INSTALLER_CHARTS=(base gateways istio-cni istiocoredns istio-telemetry istio-control istio-policy security)

function usage() {
  echo "$0
    -o <path> path where output/artifacts are stored  (required)
    -v <ver>  version for istio/installer branch      (optional)"
  exit 1
}

function die() {
    echo "$*" 1>&2 ; exit 1;
}

while getopts o:v:d: arg ; do
  case "${arg}" in
    o) OUTPUT_DIR="${OPTARG}";;
    v) INSTALLER_VERSION="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_DIR}" ]] && usage

set -x

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
OPERATOR_BASE_DIR="${SCRIPT_DIR}/.."

INSTALLER_SHA=$(cat "${OPERATOR_BASE_DIR}/installer.sha")
INSTALLER_VERSION=${INSTALLER_VERSION:-"${INSTALLER_SHA}"}

mkdir -p "${OUTPUT_DIR}"

function copy_installer_charts() {
    if [[ -z "${INSTALLER_DIR:-}" ]]; then
      # installer dir not specified, clone from github
      INSTALLER_DIR=$(mktemp -d)

      git clone https://github.com/istio/installer.git "${INSTALLER_DIR}"

      pushd .
      cd "${INSTALLER_DIR}"
      git fetch
      git checkout "${INSTALLER_VERSION}"
      popd
    fi
    local OUTPUT_CHARTS_DIR="${OUTPUT_DIR}/charts"
    mkdir -p "${OUTPUT_CHARTS_DIR}"

    for chart in "${INSTALLER_CHARTS[@]}"
    do
	    cp -R "${INSTALLER_DIR}/${chart}" "${OUTPUT_CHARTS_DIR}"
    done
}

function copy_operator_data() {
    cp -R "${OPERATOR_BASE_DIR}/data/profiles" "${OUTPUT_DIR}"
    cp -R "${OPERATOR_BASE_DIR}/data/examples" "${OUTPUT_DIR}"
    cp "${OPERATOR_BASE_DIR}/data/versions.yaml" "${OUTPUT_DIR}"
}

copy_installer_charts
copy_operator_data
