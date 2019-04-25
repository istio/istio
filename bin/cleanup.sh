#!/bin/bash -xe
# Cleanup all namespaces

for namespace in istio-system istio-control istio-control-master istio-ingress istio-telemetry; do
    kubectl delete namespace $namespace --wait --ignore-not-found
done

ACTIVE_NAMESPACES=$(kubectl get namespaces --no-headers -l istio-env -o=custom-columns=NAME:.metadata.name)
for ns in $ACTIVE_NAMESPACES; do
    kubectl label namespaces ${ns} istio-env-
done