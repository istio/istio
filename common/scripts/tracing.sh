#!/bin/bash

# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make update-common".

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

# Usage: tracing::extract_prow_trace.
# If running in a prow job, this sets the parent trace to the same value Prow tracing will use, as defined in https://github.com/kubernetes/test-infra/issues/30010
function tracing::extract_prow_trace() {
  if [[ "${PROW_JOB_ID:-}" != "" ]]; then
    local trace
    trace="$(<<< "$PROW_JOB_ID" tr -d '\-')"
    local span
    span="${trace:0:16}"
    export TRACEPARENT="01-${trace}-${span}-00"
  fi
}

function _genattrs() {
  # No upstream standard, so copy from https://github.com/jenkinsci/opentelemetry-plugin/blob/master/docs/job-traces.md
  if [[ -n "${PULL_NUMBER:=}" ]]
  then
    # Presubmit
    url="https://prow.istio.io/view/gs/istio-prow/pr-logs/pull/${REPO_OWNER}_${REPO_NAME}/${PULL_NUMBER}/${JOB_NAME}/${BUILD_ID},"
  else
    # Postsubmit or periodic
    url="https://prow.istio.io/view/gs/istio-prow/pr-logs/${JOB_NAME}/${BUILD_ID},"
  fi
  # Use printf instead of echo to avoid spaces between args
  printf '%s' "ci.pipeline.id=${JOB_NAME},"\
    "ci.pipeline.type=${JOB_TYPE},"\
    "ci.pipeline.run.url=${url}"\
    "ci.pipeline.run.number=${BUILD_ID},"\
    "ci.pipeline.run.id=${PROW_JOB_ID},"\
    "ci.pipeline.run.repo=${REPO_OWNER:-unknown}/${REPO_NAME:-unknown},"\
    "ci.pipeline.run.base=${PULL_BASE_REF:-none},"\
    "ci.pipeline.run.pull_number=${PULL_NUMBER:-none},"\
    "ci.pipeline.run.pull_sha=${PULL_PULL_SHA:-${PULL_BASE_SHA:-none}}"
}

# Usage: tracing::run <span name> [command ...]
function tracing::run() {
  # If not running in a prow job or otel-cli is not available (e.g. build system without otel-cli) just run the command
  if [ -z "${JOB_NAME:-}" ] || ! command -v otel-cli &> /dev/null
  then
    "${@:2}"
    return "$?"
  fi

  # Disable execution tracing to avoid noise
  { [[ $- = *x* ]] && was_execution_trace=1 || was_execution_trace=0; } 2>/dev/null
  { set +x; } 2>/dev/null
  # Throughout, "local" usage is critical to avoid nested calls overwriting things
  local start
  start="$(date -u +%s.%N)"
  # First, get a trace and span ID. We need to get one now so we can propagate it to the child
  # Get trace ID from TRACEPARENT, if present
  local tid
  tid="$(<<<"${TRACEPARENT:-}" cut -d- -f2)"
  tid="${tid:-"$(tr -dc 'a-f0-9' < /dev/urandom | head -c 32)"}"
  # Always generate a new span ID
  local sid
  sid="$(tr -dc 'a-f0-9' < /dev/urandom | head -c 16)"

  # Execute the command they wanted with the propagation through TRACEPARENT
  if [[ $was_execution_trace == 1 ]]; then
    { set -x; } 2>/dev/null
  fi

  TRACEPARENT="00-${tid}-${sid}-01" "${@:2}"
  local result="$?"
  { set +x; } 2>/dev/null

  local end
  end="$(date -u +%s.%N)"

  # Now report this span. We override the IDs to the ones we set before.
  otel-cli span \
    --service "${BASH_SOURCE[-1]}" \
    --name "$1" \
    --start "$start" \
    --end "$end" \
    --force-trace-id "$tid" \
    --force-span-id "$sid" \
    --attrs "$(_genattrs)"
  if [[ $was_execution_trace == 1 ]]; then
    { set -x; } 2>/dev/null
  fi
  return "$result"
}

# Usage: tracing::decorate <function>
# Automatically makes a function traced.
function tracing::decorate() {
eval "\
function $1() {
_$(typeset -f "$1")
tracing::run '$1' _$1
}
"
}
