#!/bin/bash

sh ./mtls-go-example.sh "httpbin.example.com" "httpbin.example.com"

kubectl create -n istio-system secret generic httpbin-credential \
--from-file=key=httpbin.example.com/3_application/private/httpbin.example.com.key.pem \
--from-file=cert=httpbin.example.com/3_application/certs/httpbin.example.com.cert.pem

kubectl get -n istio-system secret httpbin-credential