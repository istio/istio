#!/usr/bin/env bash

# EVAL
eval "cat <<EOF
$(</etc/istio/proxy/envoy_mixer.json)
EOF
" > /etc/istio/proxy/envoy_mixer.json

# EVAL
eval "cat <<EOF
$(</etc/istio/proxy/envoy_mixer_auth.json)
EOF
" > /etc/istio/proxy/envoy_mixer_auth.json

# PILOT-AGENT
/usr/local/bin/pilot-agent $@