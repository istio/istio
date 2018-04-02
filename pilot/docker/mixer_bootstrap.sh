#!/usr/bin/env bash

eval "cat <<EOF
$(</etc/istio/proxy/envoy_istio_policy.json)
EOF
" > /etc/istio/proxy/envoy_istio_policy.json

eval "cat <<EOF
$(</etc/istio/proxy/envoy_istio_policy_auth.json)
EOF
" > /etc/istio/proxy/envoy_istio_policy_auth.json

eval "cat <<EOF
$(</etc/istio/proxy/envoy_istio_telemetry.json)
EOF
" > /etc/istio/proxy/envoy_istio_telemetry.json

eval "cat <<EOF
$(</etc/istio/proxy/envoy_istio_telemetry_auth.json)
EOF
" > /etc/istio/proxy/envoy_istio_telemetry_auth.json

# PILOT-AGENT
/usr/local/bin/pilot-agent $@