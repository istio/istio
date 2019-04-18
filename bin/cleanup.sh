#!/bin/bash -xe
# Cleanup all namespaces

for namespace in istio-system istio-control istio-control-master istio-ingress istio-telemetry; do
    kubectl delete namespace $namespace --wait --ignore-not-found
done
