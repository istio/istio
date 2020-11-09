#!/usr/bin/env bash
# Copyright Istio Authors. All Rights Reserved.
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

function usage() {
  echo "$0
    -o <path> path where output/artifacts are stored  (required)"
  exit 1
}

function die() {
    echo "$*" 1>&2 ; exit 1;
}

while getopts o:v:d: arg ; do
  case "${arg}" in
    o) OUTPUT_DIR="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_DIR}" ]] && usage

set -x

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
INSTALLER_DIR="${SCRIPT_DIR}/../../manifests"

mkdir -p "${OUTPUT_DIR}"

cp -R "${INSTALLER_DIR}/charts" "${OUTPUT_DIR}"
cp -R "${INSTALLER_DIR}/profiles" "${OUTPUT_DIR}"

