#!/bin/bash

set -x

INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].port}')
export SECURE_INGRESS_PORT

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://httpbin.example.com:"$SECURE_INGRESS_PORT"/status/418