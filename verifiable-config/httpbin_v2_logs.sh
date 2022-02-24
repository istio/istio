#!/bin/bash

export V2_POD=$(kubectl get pod -l app=httpbin,version=v2 -o jsonpath={.items..metadata.name})
echo -e "kubectl logs "$V2_POD" -c httpbin -f"
kubectl logs "$V2_POD" -c httpbin -f
