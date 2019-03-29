#!/bin/sh
set -e

rm -f stop_connection_test

export INGRESS_HOST=$(kubectl -n istio-ingress get service ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESS_PORT=$(kubectl -n istio-ingress get service ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].port}')
export PRODUCTPAGE_URL=http://${INGRESS_HOST}:${INGRESS_PORT}/productpage
while [ ! -f stop_connection_test ]
do
    RESULT=$(curl -s -o /dev/null -w "%{http_code}" $PRODUCTPAGE_URL)
    if [ $RESULT -ne "200"  ]; then
        echo "Got $RESULT when curl-ing $PRODUCTPAGE_URL"
        exit 1
    fi
done
