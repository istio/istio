#!/bin/bash

# Usage: tracing::init [endpoint; default localhost:4317]
function tracing::init() {
  export OTEL_EXPORTER_OTLP_ENDPOINT="${1:-${OTEL_EXPORTER_OTLP_ENDPOINT:-localhost:4317}}"
}

# Usage: tracing::run <span name> [command ...]
function tracing::run() {
  function gen-uid() {
    tr -dc 'a-f0-9' < /dev/urandom | head -c$1
  }
  # Throughout, "local" usage is critical to avoid nested calls overwriting things
  local start="$(date -u +%s.%N)"
  # First, get a trace and span ID. We need to get one now so we can propogate it to the child
  # Get trace ID from TRACEPARENT, if present
  local tid="$(<<<${TRACEPARENT:-} cut -d- -f2)"
  tid="${tid:-"$(gen-uid 32)"}"
  # Always generate a new span ID
  local sid="$(gen-uid 16)"

  # Execute the command they wanted with the propagation through TRACEPARENT
  TRACEPARENT="00-${tid}-${sid}-01" "${@:2}"

  local end="$(date -u +%s.%N)"

  # Now report this span. We override the IDs to the ones we set before.
  # TODO: support attributes
  otel-cli span \
    --service ${BASH_SOURCE[-1]} \
    --name "$1" \
    --start "$start" \
    --end "$end" \
    --force-trace-id "$tid" \
    --force-span-id "$sid"
}
