#!/bin/bash

source export-ingress-host-port-gke.sh

echo "curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:${SECURE_INGRESS_PORT}:${INGRESS_HOST} \
--cacert httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem \
--cert httpbin.example.com/4_client/certs/httpbin.example.com.cert.pem \
--key httpbin.example.com/4_client/private/httpbin.example.com.key.pem \
https://httpbin.example.com:${SECURE_INGRESS_PORT}/status/418"

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem \
--cert httpbin.example.com/4_client/certs/httpbin.example.com.cert.pem \
--key httpbin.example.com/4_client/private/httpbin.example.com.key.pem \
https://httpbin.example.com:"$SECURE_INGRESS_PORT"/status/418