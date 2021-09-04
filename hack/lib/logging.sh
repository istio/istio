#!/usr/bin/env bash

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

# This file is copy form kubernetes.
# Controls verbosity of the script output and logging.
ISTIO_VERBOSE="${ISTIO_VERBOSE:-2}"

# Handler for when we exit automatically on an error.
# Borrowed from https://gist.github.com/ahendrix/7030300
ISTIO::log::errexit() {
  local err="${PIPESTATUS[*]}"

  # If the shell we are in doesn't have errexit set (common in subshells) then
  # don't dump stacks.
  set +o | grep -qe "-o errexit" || return

  set +o xtrace
  local code="${1:-1}"
  # Print out the stack trace described by $function_stack
  if [ ${#FUNCNAME[@]} -gt 2 ]
  then
    ISTIO::log::error "Call tree:"
    for ((i=1;i<${#FUNCNAME[@]}-1;i++))
    do
      ISTIO::log::error " ${i}: ${BASH_SOURCE[${i}+1]}:${BASH_LINENO[${i}]} ${FUNCNAME[${i}]}(...)"
    done
  fi
  ISTIO::log::error_exit "Error in ${BASH_SOURCE[1]}:${BASH_LINENO[0]}. '${BASH_COMMAND}' exited with status ${err}" "${1:-1}" 1
}

ISTIO::log::install_errexit() {
  # trap ERR to provide an error handler whenever a command exits nonzero  this
  # is a more verbose version of set -o errexit
  trap 'ISTIO::log::errexit' ERR

  # setting errtrace allows our ERR trap handler to be propagated to functions,
  # expansions and subshells
  set -o errtrace
}

# Log an error and exit.
# Args:
#   $1 Message to log with the error
#   $2 The error code to return
#   $3 The number of stack frames to skip when printing.
ISTIO::log::error_exit() {
  local message="${1:-}"
  local code="${2:-1}"
  local stack_skip="${3:-0}"
  stack_skip=$((stack_skip + 1))

  if [[ ${ISTIO_VERBOSE} -ge 4 ]]; then
    local source_file=${BASH_SOURCE[${stack_skip}]}
    local source_line=${BASH_LINENO[$((stack_skip - 1))]}
    echo "!!! Error in ${source_file}:${source_line}" >&2
    [[ -z ${1-} ]] || {
      echo "  ${1}" >&2
    }

    ISTIO::log::stack ${stack_skip}

    echo "Exiting with status ${code}" >&2
  fi

  exit "${code}"
}

# Log an error but keep going.  Don't dump the stack or exit.
ISTIO::log::error() {
  timestamp=$(date +"[%m%d %H:%M:%S]")
  echo "!!! ${timestamp} ${1-}" >&2
  shift
  for message; do
    echo "    ${message}" >&2
  done
}

ISTIO::log::usage_from_stdin() {
  local messages=()
  while read -r line; do
    messages+=("${line}")
  done

  ISTIO::log::usage "${messages[@]}"
}
