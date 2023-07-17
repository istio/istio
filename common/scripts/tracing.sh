#!/bin/bash

# Usage: tracing::init [endpoint; default localhost:4317]
function tracing::init() {
  export OTEL_EXPORTER_OTLP_ENDPOINT="${1:-${OTEL_EXPORTER_OTLP_ENDPOINT:-localhost:4317}}"
}

function _genattrs() {
  # No upstream standard, so copy from https://github.com/jenkinsci/opentelemetry-plugin/blob/master/docs/job-traces.md
  echo "ci.pipeline.id=${JOB_NAME},"\
    "ci.pipeline.type=${JOB_TYPE},"\
    "ci.pipeline.run.url=https://prow.istio.io/view/gs/istio-prow/pr-logs/pull/${REPO_OWNER}_${REPO_NAME}/${PULL_NUMBER}/${JOB_NAME}/${BUILD_ID},"\
    "ci.pipeline.run.number=${BUILD_ID},"\
    "ci.pipeline.run.id=${PROW_JOB_ID},"\
    "ci.pipeline.run.repo=${REPO_OWNER}/${REPO_NAME},"\
    "ci.pipeline.run.base=${PULL_BASE_REF},"\
    "ci.pipeline.run.pull_number=${PULL_NUMBER},"\
    "ci.pipeline.run.pull_sha=${PULL_PULL_SHA}"
}

# Usage: tracing::run <span name> [command ...]
function tracing::run() {
  # Disable execution tracing to avoid noise
  { [[ $- = *x* ]] && was_execution_trace=1 || was_execution_trace=0; } 2>/dev/null
  { set +x; } 2>/dev/null
  # Throughout, "local" usage is critical to avoid nested calls overwriting things
  local start="$(date -u +%s.%N)"
  # First, get a trace and span ID. We need to get one now so we can propagate it to the child
  # Get trace ID from TRACEPARENT, if present
  local tid="$(<<<${TRACEPARENT:-} cut -d- -f2)"
  tid="${tid:-"$(tr -dc 'a-f0-9' < /dev/urandom | head -c 32)"}"
  # Always generate a new span ID
  local sid="$(tr -dc 'a-f0-9' < /dev/urandom | head -c 16)"

  # Execute the command they wanted with the propagation through TRACEPARENT
  if [[ $was_execution_trace == 1 ]]; then
    { set -x; } 2>/dev/null
  fi
  TRACEPARENT="00-${tid}-${sid}-01" "${@:2}"
  { set +x; } 2>/dev/null

  local end="$(date -u +%s.%N)"

  # Now report this span. We override the IDs to the ones we set before.
  # TODO: support attributes
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
