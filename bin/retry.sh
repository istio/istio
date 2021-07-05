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

# retry.sh retries a command until it succeeds. It accepts a regex pattern to match failures on to
# determine if a retry should be attempted.
# Example: retry.sh "connection timed out" ./my-flaky-script.sh some args
# This will run "my-flaky-script.sh", retrying any failed runs that output "connection timed out" up
# to 5 times.

function fail {
  echo "${1}" >&2
  exit 1
}

function isatty() {
 if [ -t 1 ] ; then
   return 0
  else
   return 1
  fi
}

function retry {
  local tmpFile
  tmpFile=$(mktemp)
  trap 'rm -f "${tmpFile}"' EXIT

  local failureRegex="$1"
  shift
  local n=1
  local max=5
  while true; do
    unset SHELL # Don't let environment control which shell to use
    if isatty; then
      if [ "$(uname)" == "Darwin" ]; then
        script -q -r "${tmpFile}" "${*}"
      else
        script --flush --quiet --return "${tmpFile}" --command "${*}"
      fi
    else
      # if we aren't a TTY, run directly as script will always run with a tty, which may output content that
      # we cannot display
      set -o pipefail; "$@" 2>&1 | tee "${tmpFile}"
    fi
    # shellcheck disable=SC2181
    if [[ $? == 0 ]]; then
      break
    fi
    if ! grep -Eq "${failureRegex}" "${tmpFile}"; then
      fail "Unexpected failure"
    fi
    if [[ $n -lt $max ]]; then
      ((n++))
      echo "Command failed. Attempt $n/$max:"
    else
      fail "The command has failed after $n attempts."
    fi
  done
}

retry "$@"
