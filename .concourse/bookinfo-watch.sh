#!/bin/bash
# This script runs the commands passed to it in parallel to a curl against the bookinfo application
# If either the command or the bookinfo test fails, the script exists with an errorneous code

CMD=$*
# Run command in background
touch command_running
bash -cxe "trap 'rm -f  command_running' EXIT; $CMD" &
CMD_PID=$!
GPID=$(ps -o pgid= $CMD_PID)
# In case of CTRL-C this will also kill the background process
trap "set -x; pkill -g $GPID; exit 1" SIGINT

# Test if bookinfo is working
export INGRESS_HOST=$(kubectl -n istio-ingress get service ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESS_PORT=$(kubectl -n istio-ingress get service ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].port}')
export PRODUCTPAGE_URL=http://${INGRESS_HOST}:${INGRESS_PORT}/productpage
while [ -f command_running ]
do
    RESULT=$(curl -v -s -o /tmp/out -w "%{http_code}" $PRODUCTPAGE_URL 2> /tmp/err)
    if [ "$RESULT" -ne "200"  ]; then
        echo "Got $RESULT when curl-ing $PRODUCTPAGE_URL"
        cat /tmp/err
        cat /tmp/out
        exit 1
    fi
done

wait $CMD_PID
RTC=$?
echo "bookinfo-watch exited with $RTC"
exit $RTC