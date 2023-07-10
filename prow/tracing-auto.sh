#!/bin/bash

# Usage: tracing::init [endpoint; default localhost:4317]
function tracing::init() {
  export OTEL_EXPORTER_OTLP_ENDPOINT="${1:-${OTEL_EXPORTER_OTLP_ENDPOINT:-localhost:4317}}"
}

# Usage: tracing::auto::init [endpoint; default localhost:4317]
function tracing::auto::init() {
  tracing::init
  set -o functrace

  # First, get a trace and span ID. We need to get one now so we can propogate it to the child
  # Get trace ID from TRACEPARENT, if present
  trace="$(<<<${TRACEPARENT:-} cut -d- -f2)"
  trace="${trace:-"$(tr -dc 'a-f0-9' < /dev/urandom | head -c 32)"}"
  spans=()
  starts=()
  oldparents=()
  function tracing::internal::on_debug() {
      # We will get a callback for each operation in the function. We just want it for the function initially entering
      # This is probably not handling recursive calls correctly.
      [[ "${FUNCNAME[1]}" != "$(<<<$BASH_COMMAND cut -d' ' -f1)" ]] && return
      local tp=`tr -dc 'a-f0-9' < /dev/urandom | head -c 16`
      spans+=("$tp")
      starts+=("$(date -u +%s.%N)")
      oldparents+=("${TRACEPARENT:-}")
      TRACEPARENT="00-${trace}-${tp}-01"
  }
  function tracing::internal::on_return() {
      if [[ ${#spans[@]} == 0 ]]; then
        # This happens on the call to tracing::init
        return
      fi
      local tp=""
      if [[ ${#spans[@]} -gt 1 ]]; then
        tp="00-${trace}-${spans[-2]}-01"
      fi
      local span="${spans[-1]}"
      local start="${starts[-1]}"
      local nextparent="${oldparents[-1]}"
      unset spans[-1]
      unset starts[-1]
      unset oldparents[-1]

      TRACEPARENT=$nextparent otel-cli span \
        --service "${BASH_SOURCE[-1]}" \
        --name "${FUNCNAME[1]}" \
        --start "$start" \
        --end "$(date -u +%s.%N)" \
        --force-trace-id "$trace" \
        --force-span-id "$span"
      TRACEPARENT="${nextparent}"
  }

  trap tracing::internal::on_return RETURN
  trap tracing::internal::on_debug DEBUG
}

# Usage: tracing::run <span name> [command ...]
function tracing::run() {
  # Throughout, "local" usage is critical to avoid nested calls overwriting things
  local start="$(date -u +%s.%N)"
  # First, get a trace and span ID. We need to get one now so we can propagate it to the child
  # Get trace ID from TRACEPARENT, if present
  local tid="$(<<<${TRACEPARENT:-} cut -d- -f2)"
  tid="${tid:-"$(tr -dc 'a-f0-9' < /dev/urandom | head -c 32)"}"
  # Always generate a new span ID
  local sid="$(tr -dc 'a-f0-9' < /dev/urandom | head -c 16)"

  # Execute the command they wanted with the propagation through TRACEPARENT
  TRACEPARENT="00-${tid}-${sid}-01" "${@:2}"

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
