#!/bin/bash

# Usage: tracing::init [endpoint; default localhost:4317]
function tracing::init() {
  export OTEL_EXPORTER_OTLP_ENDPOINT="${1:-${OTEL_EXPORTER_OTLP_ENDPOINT:-localhost:4317}}"
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
    --force-span-id "$sid"
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
