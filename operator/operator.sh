#!/bin/bash
# TODO(sdake) write a proper controller to watch
# for configmap changes and apply them selectively or delete them

while true; do
    kubectl apply -f /etc/istio/operator/config
    sleep 5 # ugh
done
