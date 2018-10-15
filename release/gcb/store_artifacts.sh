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

source "/workspace/gcb_env.sh"

# This script takes files from a specified directory and uploads
# then to GCS

function usage() {
  echo "$0
    uses CB_OUTPUT_PATH CB_GCS_BUILD_PATH"
  exit 1
}

[[ -z "${CB_OUTPUT_PATH}" ]] && usage
[[ -z "${CB_GCS_BUILD_PATH}" ]] && usage

#copy to gcs
gsutil -m cp -r "${CB_OUTPUT_PATH}"/* "gs://${CB_GCS_BUILD_PATH}/"
