#!/bin/bash

set -x

INGRESS_HOST=$(kubectl -n istio-system get pod -l istio=ingressgateway -o jsonpath='{.items[0].status.hostIP}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.port==443)].nodePort}')
export SECURE_INGRESS_PORT

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://httpbin.example.com:"$SECURE_INGRESS_PORT"/status/418