#!/bin/bash

echo "restore the credentials for httpbin"

kubectl -n istio-system delete secret httpbin-credential

kubectl create -n istio-system secret generic httpbin-credential \
--from-file=key=httpbin.example.com/3_application/private/httpbin.example.com.key.pem \
--from-file=cert=httpbin.example.com/3_application/certs/httpbin.example.com.cert.pem