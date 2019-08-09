#!/bin/bash

source export-ingress-host-port-gke.sh

echo "curl -v -HHost:helloworld-v1.example.com \
--resolve helloworld-v1.example.com:${SECURE_INGRESS_PORT}:${INGRESS_HOST} \
--cacert helloworld-v1.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://helloworld-v1.example.com:${SECURE_INGRESS_PORT}/hello"

curl -v -HHost:helloworld-v1.example.com \
--resolve helloworld-v1.example.com:$SECURE_INGRESS_PORT:$INGRESS_HOST \
--cacert helloworld-v1.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://helloworld-v1.example.com:$SECURE_INGRESS_PORT/hello