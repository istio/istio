#!/bin/bash
INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
curl $INGRESS_HOST/ip -s -o /dev/null -w "%{http_code}\n"
