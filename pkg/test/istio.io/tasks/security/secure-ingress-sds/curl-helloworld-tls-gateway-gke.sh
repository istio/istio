#!/bin/bash

INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "INGRESS_HOST ${INGRESS_HOST}"
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].port}')
echo "SECURE_INGRESS_PORT ${SECURE_INGRESS_PORT}"
export SECURE_INGRESS_PORT

echo "curl -v -HHost:helloworld-v1.example.com \
--resolve helloworld-v1.example.com:${SECURE_INGRESS_PORT}:${INGRESS_HOST} \
--cacert helloworld-v1.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://helloworld-v1.example.com:${SECURE_INGRESS_PORT}/hello"

curl -v -HHost:helloworld-v1.example.com \
--resolve helloworld-v1.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert helloworld-v1.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://helloworld-v1.example.com:"$SECURE_INGRESS_PORT"/hello