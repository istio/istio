#! /bin/bash
set -x
set -e

NAMESPACE=e2e
# Hacky shortcut to switch everything to a different namespace without editing
# every command here and in the Makefile
kubectl config set-context $(kubectl config current-context) --namespace=$NAMESPACE
make NAMESPACE=$NAMESPACE # default target is istio injected svc and normal client
kubectl get all
cli=$(kubectl get pod -l app=fortio -o jsonpath='{.items[0].metadata.name}')
cliIp=$(kubectl get pod -l app=fortio -o jsonpath='{.items[0].status.podIP}')
srv1=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[0].status.podIP}')
srv2=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[1].status.podIP}')
debugurlsuffix=":8080/debug?env=dump"

# Direct pod ip to pod ip access:
url1="http://$srv1$debugurlsuffix"
url2="http://$srv2$debugurlsuffix"
singlecall="/usr/local/bin/fortio -- load -loglevel verbose -c 1 -qps 0 -t 1ns"
kubectl exec $cli $singlecall "$url1"
kubectl exec $cli $singlecall "$url2"
grpcping="/usr/local/bin/fortio -- grpcping -loglevel warning -n 100"
kubectl exec $cli $grpcping $cliIp # localhost call
kubectl exec $cli $grpcping $srv1
kubectl exec $cli $grpcping $srv2

# svc access:
kubectl exec $cli $singlecall "http://echosrv$debugurlsuffix"
