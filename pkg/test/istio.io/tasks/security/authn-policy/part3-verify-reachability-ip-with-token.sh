#!/bin/bash
INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
TOKEN=$(curl https://raw.githubusercontent.com/istio/istio/master/security/tools/jwt/samples/demo.jwt -s)
curl --header "Authorization: Bearer $TOKEN" $INGRESS_HOST/ip -s -o /dev/null -w "%{http_code}\n"
