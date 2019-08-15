#!/bin/bash

sh ./mtls-go-example.sh "helloworld-v1.example.com" "helloworld-v1.example.com"

kubectl create -n istio-system secret generic helloworld-credential \
--from-file=key=helloworld-v1.example.com/3_application/private/helloworld-v1.example.com.key.pem \
--from-file=cert=helloworld-v1.example.com/3_application/certs/helloworld-v1.example.com.cert.pem

kubectl get -n istio-system secret helloworld-credential