#!/bin/bash

echo "Delete the gateway’s secret and create a new one to change the ingress gateway’s credentials."

kubectl -n istio-system delete secret httpbin-credential

sh ./mtls-go-example.sh "httpbin.new.example.com"

kubectl create -n istio-system secret generic httpbin-credential \
--from-file=key=httpbin.new.example.com/3_application/private/httpbin.example.com.key.pem \
--from-file=cert=httpbin.new.example.com/3_application/certs/httpbin.example.com.cert.pem

kubectl get -n istio-system secret httpbin-credential