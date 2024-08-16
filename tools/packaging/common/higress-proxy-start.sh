#!/bin/bash

if [ -n "$LITE_METRICS" ]; then
    cp /var/lib/istio/envoy/envoy_bootstrap_lite_tmpl.json /var/lib/istio/envoy/envoy_bootstrap_tmpl.json
fi

nohup supercronic /var/lib/istio/cron.txt &> /dev/null &

/usr/local/bin/pilot-agent $*

