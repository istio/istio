#!/bin/bash

kubectl delete -f httpbin.yaml
kubectl delete virtualservice httpbin
kubectl delete destinationrule httpbin
kubectl delete authorizationpolicies deny-method-get
