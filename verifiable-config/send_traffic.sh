#!/bin/bash

export SLEEP_POD=$(kubectl get pod -l app=sleep -o jsonpath={.items..metadata.name})
echo -e "kubectl exec "${SLEEP_POD}" -c sleep -- curl -sS http://httpbin:8000/headers"
kubectl exec "${SLEEP_POD}" -c sleep -- curl -sS http://httpbin:8000/
