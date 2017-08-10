#! /bin/bash
set -x
set -e
FORTIO_IMAGE='gcr.io/istio-testing/fortio:0.2.1-pre1' # escape / needed

sed -e s!FORTIO_IMAGE!$FORTIO_IMAGE!g < k8services.yaml.in | kubectl apply -f -
kubectl get -o wide pods
cli=$(kubectl get pod -l app=fortio -o jsonpath='{.items[0].metadata.name}')
srv1=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[0].status.podIP}')
srv2=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[1].status.podIP}')
debugurlsuffix=":8080/debug?env=dump"
url1="http://$srv1$debugurlsuffix"
url2="http://$srv2$debugurlsuffix"
singlecall="/usr/local/bin/fortio -- load -loglevel verbose -c 1 -qps 0 -t 1ns"
kubectl exec $cli $singlecall "$url1"
kubectl exec $cli $singlecall "$url2"
