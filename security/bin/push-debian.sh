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

# Example usage:
#
# bin/push-debian.sh \
#   -c opt
#   -v 0.2.1
#   -p gs://istio-release/release/0.2.1/deb

set -o errexit
set -o pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
VERSION_FILE="${ROOT}/tools/deb/version"
BAZEL_ARGS=()
BAZEL_TARGET='//security/tools/deb:istio-auth-node-agent'
BAZEL_BINARY="${ROOT}/../bazel-bin/security/tools/deb/istio-auth-node-agent"
GCS_PATH=""
OUTPUT_DIR=""

set -ex

function usage() {
  echo "$0 \
    -c <bazel config to use> \
    -o directory to copy files \
    -p <GCS path, e.g. gs://istio-release/release/0.2.1/deb> \
    -v <istio version number>"
  exit 1
}

while getopts ":c:o:p:v:" arg; do
  case ${arg} in
    c) BAZEL_ARGS+=("--config=${OPTARG}");;
    o) OUTPUT_DIR="${OPTARG}";;
    p) GCS_PATH="${OPTARG}";;
    v) ISTIO_VERSION="${OPTARG}";;
    *) usage;;
  esac
done

if [[ -n "${ISTIO_VERSION}" ]]; then
  BAZEL_TARGET+='-release'
  BAZEL_BINARY+='-release'
  echo "${ISTIO_VERSION}" > "${VERSION_FILE}"
  trap 'rm "${VERSION_FILE}"' EXIT
fi

[[ -z "${GCS_PATH}" ]] && [[ -z "${OUTPUT_DIR}" ]] && usage

# might need: --incompatible_disallow_set_constructor=false
bazel build ${BAZEL_ARGS[@]} ${BAZEL_TARGET}

if [[ "${GCS_PATH}" != "" ]]; then
  gsutil -m cp -r "${BAZEL_BINARY}.deb" ${GCS_PATH}/
fi

if [[ "${OUTPUT_DIR}" != "" ]]; then
  cp "${BAZEL_BINARY}.deb" "${OUTPUT_DIR}/"
fi
