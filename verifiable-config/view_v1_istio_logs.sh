#!/bin/bash

export V1_POD=$(kubectl get pod -l app=httpbin,version=v1 -o jsonpath={.items..metadata.name})
echo -e "kubectl logs "$V1_POD" -c istio-proxy -f"
kubectl logs "$V1_POD" -c istio-proxy
