#! /bin/bash
set -x
set -e
FORTIO_IMAGE='gcr.io/istio-testing/fortio:0.2.1-pre1' # escape / needed

sed -e s!FORTIO_IMAGE!$FORTIO_IMAGE!g < k8services.yaml.in | kubectl apply -f -
kubectl get -o wide pods
cli=$(kubectl get pod -l app=fortio -o jsonpath='{.items[0].metadata.name}')
srv1=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[0].metadata.name}')
srv2=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[1].metadata.name}')
url1="http://$srv1:8080/debug?env=dump"
kubectl exec $cli /usr/local/bin/fortio -- load -loglevel verbose "$url1"
