#!/bin/bash

# Copyright Istio Authors
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

# If CF_CREDENTIALS (a JSON blob with R2 credentials, set by Prow) is present,
# translate it into the standard AWS_* env vars that `aws s3` expects. No-op if
# AWS_ACCESS_KEY_ID is already set or CF_CREDENTIALS is empty, so local dev
# flows that export AWS_* directly are unaffected.
# Example CF_CREDENTIALS value:
# {
#   "access_key": "AKIAIOSFODNN7EXAMPLE",
#   "secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
#   "session_token": "AQoDYXdzEJr...<remainder of security token>",
#   "region": "auto",
#   "endpoint": "https://12345.r2.cloudflarestorage.com"
# }
function setup_cf_credentials () {
  if [[ -z "${CF_CREDENTIALS:-}" || -n "${AWS_ACCESS_KEY_ID:-}" ]]; then
    return 0
  fi
  local _xtrace=0
  case $- in *x*) _xtrace=1;; esac
  { set +x; } 2>/dev/null
  AWS_ACCESS_KEY_ID="$(echo "${CF_CREDENTIALS}" | jq -r '.access_key' | tr -d '\n')"
  AWS_SECRET_ACCESS_KEY="$(echo "${CF_CREDENTIALS}" | jq -r '.secret_key' | tr -d '\n')"
  AWS_SESSION_TOKEN="$(echo "${CF_CREDENTIALS}" | jq -r '.session_token' | tr -d '\n')"
  AWS_REGION="$(echo "${CF_CREDENTIALS}" | jq -r '.region' | tr -d '\n')"
  AWS_ENDPOINT_URL="$(echo "${CF_CREDENTIALS}" | jq -r '.endpoint' | tr -d '\n')"
  export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_REGION AWS_ENDPOINT_URL
  [[ $_xtrace == 1 ]] && set -x
}
