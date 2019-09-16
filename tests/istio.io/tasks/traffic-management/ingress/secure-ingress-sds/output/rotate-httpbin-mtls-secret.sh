#!/bin/bash

echo "Delete the gateway’s secret and create a new one with client CA certificate"

kubectl -n istio-system delete secret httpbin-credential

kubectl create -n istio-system secret generic httpbin-credential  \
--from-file=key=httpbin.example.com/3_application/private/httpbin.example.com.key.pem \
--from-file=cert=httpbin.example.com/3_application/certs/httpbin.example.com.cert.pem \
--from-file=cacert=httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem

kubectl get -n istio-system secret httpbin-credential