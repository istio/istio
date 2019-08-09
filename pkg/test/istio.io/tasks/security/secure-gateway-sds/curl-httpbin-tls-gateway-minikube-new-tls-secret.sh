#!/bin/bash

source export-ingress-host-port-minikube.sh

echo "curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:${SECURE_INGRESS_PORT}:${INGRESS_HOST} \
--cacert httpbin.new.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://httpbin.example.com:${SECURE_INGRESS_PORT}/status/418"

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:$SECURE_INGRESS_PORT:$INGRESS_HOST \
--cacert httpbin.new.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://httpbin.example.com:$SECURE_INGRESS_PORT/status/418