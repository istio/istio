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

OUTPUT_ROOT_DIR=""

TEMP_DIR_DEFAULT="/tmp"
# TODO: these should be under a subdir
INSTALLER_CHARTS=(crds gateways istio-cni istiocoredns istio-telemetry istio-control istio-policy security)
INSTALLER_REPO="installer"

function usage() {
  echo "$0
    -o <path> path where output/artifacts are stored  (required)
    -v <ver>  version for istio/installer branch      (optional)
    -d <path> path to use for temp directory          (optional, randomized default is under ${TEMP_DIR_DEFAULT} )"
  exit 1
}

function die() {
    echo "$*" 1>&2 ; exit 1;
}

while getopts o:v:d: arg ; do
  case "${arg}" in
    o) OUTPUT_ROOT_DIR="${OPTARG}";;
    v) INSTALLER_VERSION="${OPTARG}";;
    d) TEMP_DIR="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_ROOT_DIR}" ]] && usage

set -x

function get_version() {
    local ver=`git rev-parse --abbrev-ref HEAD`
    if [[ "${ver}" == "HEAD" ]]; then
      echo `git describe --tags`
      return 0
    fi
    echo "${ver}"
}

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
OPERATOR_BASE_DIR="${SCRIPT_DIR}/.."
OPERATOR_VERSION=$(get_version)
TEMP_DIR=${TEMP_DIR:-"$(mktemp -d ${TEMP_DIR_DEFAULT}/istio.${OPERATOR_VERSION}.XXXXXXXX)"}

INSTALLER_VERSION=${INSTALLER_VERSION:-"${OPERATOR_VERSION}"}
OUTPUT_DIR="${OUTPUT_ROOT_DIR}/install/kubernetes/operator"

mkdir -p "${OUTPUT_DIR}"

function copy_installer_charts() {
    cd "${TEMP_DIR}"
    git clone -b "${INSTALLER_VERSION}" https://github.com/istio/${INSTALLER_REPO} || die "Could not clone installer repo at branch ${INSTALLER_VERSION}"
    local OUTPUT_CHARTS_DIR="${OUTPUT_DIR}/charts"
    mkdir -p "${OUTPUT_CHARTS_DIR}"

    for chart in "${INSTALLER_CHARTS[@]}"
    do
	    cp -R "${INSTALLER_REPO}/${chart}" "${OUTPUT_CHARTS_DIR}"
    done
}

function copy_profiles() {
    cp -R "${OPERATOR_BASE_DIR}/data/profiles" "${OUTPUT_DIR}"
}

function copy_versions_files() {
    cp "${OPERATOR_BASE_DIR}/version/version.yaml" "${OUTPUT_DIR}"
    cp "${OPERATOR_BASE_DIR}/version/versions.yaml" "${OUTPUT_DIR}"
}

copy_installer_charts
copy_profiles
copy_versions_files
