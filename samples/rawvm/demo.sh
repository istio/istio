#! /bin/bash
set -x
set -e

# This assume you ran pilot's bin/e2e.sh and/or for instance:
# bazel run //test/integration -- --logtostderr -tag ldemailly7 -n e2e -auth enable
# and have
TAG=ldemailly7
NAMESPACE=e2e
# Hacky shortcut to switch everything to a different namespace without editing
# every command here and in the Makefile
kubectl config set-context $(kubectl config current-context) --namespace=$NAMESPACE
make NAMESPACE=$NAMESPACE TAG=$TAG # default target is istio injected svc and normal client
kubectl get all
cli=$(kubectl get pod -l app=fortio -o jsonpath='{.items[0].metadata.name}')
cliIp=$(kubectl get pod -l app=fortio -o jsonpath='{.items[0].status.podIP}')
srv1Name=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[0].metadata.name}')
srv1=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[0].status.podIP}')
srv2=$(kubectl get pod -l app=echosrv -o jsonpath='{.items[1].status.podIP}')
debugurlsuffix=":8080/debug?env=dump"

# Direct pod ip to pod ip access:
url1="http://$srv1$debugurlsuffix"
url2="http://$srv2$debugurlsuffix"
singlecall="fortio -- load -H Host:echosrv -loglevel verbose -c 1 -qps 0 -t 1ns"

# Server to Server (istio injected) always works - even with Auth:
echo "*** (istio injected) svc-svc calls (from $srv1Name)"
kubectl exec $srv1Name -c echosrv $singlecall "$url1"
kubectl exec $srv1Name -c echosrv $singlecall "$url2"
# Client (non istio injected) to Server (istio injected) only works w/o Auth:
# https://github.com/istio/pilot/issues/1015
echo "*** non istio client to (istio) svc calls (from $cli)"
kubectl exec $cli $singlecall "$url1"
kubectl exec $cli $singlecall "$url2"
echo "*** grpc calls (from $cli)"
grpcping="/usr/local/bin/fortio -- grpcping -loglevel warning -n 100"
kubectl exec $cli $grpcping $cliIp # localhost call
kubectl exec $cli $grpcping $srv1
kubectl exec $cli $grpcping $srv2

# svc access:
echo "*** Access using service url (from $cli)"
kubectl exec $cli $singlecall "http://echosrv$debugurlsuffix"
