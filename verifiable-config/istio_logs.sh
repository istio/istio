#!/bin/bash

export ISTIO_POD=$(kubectl get pod -n istio-system -l app=istiod -o jsonpath={.items..metadata.name})
echo -e "kubectl logs "$ISTIO_POD" -n istio-system"
kubectl logs "$ISTIO_POD" -n istio-system
