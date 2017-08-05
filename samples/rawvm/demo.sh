#! /bin/bash
set -x
set -e
kubectl apply -f k8services.yaml
kubectl get -o wide pods
cli=$(kubectl get pod -l app=fortio -o jsonpath='{.items[0].metadata.name}')
srv1=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[0].metadata.name}')
srv2=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[1].metadata.name}')
url1="http://$srv1:8080/debug?env=dump"
kubectl exec $cli /usr/local/bin/fortio -- -loglevel verbose "$url1"
