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

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# This script takes files from a specified directory and uploads
# then to GCS, in addition it saves the source code and uploads it

GCS_PREFIX=""
OUTPUT_PATH=""

function usage() {
  echo "$0
    -o <path> src path where build output/artifacts were stored (required)
    -p <name> GCS bucket & prefix path where to store build     (required)"
  exit 1
}

while getopts o:p: arg ; do
  case "${arg}" in
    o) OUTPUT_PATH="${OPTARG}";;
    p) GCS_PREFIX="${OPTARG}";;
    *) usage;;
  esac
done

[[ -z "${OUTPUT_PATH}" ]] && usage
[[ -z "${GCS_PREFIX}"  ]] && usage

# remove any trailing / for GCS
GCS_PREFIX=${GCS_PREFIX%/}
GCS_PATH="gs://${GCS_PREFIX}"

# preserve the source from the root of the code
pushd "${ROOT}/../../../.."
# tar the source code
tar -czf "${OUTPUT_PATH}/source.tar.gz" go src --exclude go/out --exclude go/bin
popd

#copy to gcs
gsutil -m cp -r "${OUTPUT_PATH}"/* "${GCS_PATH}/"
