#!/bin/bash
TOKEN=$(curl https://raw.githubusercontent.com/istio/istio/master/security/tools/jwt/samples/demo.jwt -s)
kubectl exec $(kubectl get pod -l app=sleep -n foo -o jsonpath={.items..metadata.name}) -c sleep -n foo -- curl http://httpbin.foo:8000/ip -s -o /dev/null -w "%{http_code}\n" --header "Authorization: Bearer $TOKEN"
