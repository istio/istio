#!/bin/bash

# Copyright Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -x
set -e

# Source istio.VERSION
# shellcheck disable=SC1091
source ../../istio.VERSION
# This assume you installed the control plane already
# kubectl apply -f install/kube/istio-auth.yaml
NAMESPACE=fortio
# Hacky shortcut to switch everything to a different namespace without editing
# every kubectl command here to add -n $(NAMESPACE)
kubectl config set-context "$(kubectl config current-context)" --namespace=$NAMESPACE
make NAMESPACE=$NAMESPACE TAG="$TAG" # default target is istio injected svc and normal client
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
kubectl exec "$srv1Name" -c echosrv "$singlecall" "$url1"
kubectl exec "$srv1Name" -c echosrv "$singlecall" "$url2"
# Client (non istio injected) to Server (istio injected) only works w/o Auth:
# https://github.com/istio/pilot/issues/1015
echo "*** non istio client to (istio) svc calls (from $cli)"
kubectl exec "$cli" "$singlecall" "$url1"
kubectl exec "$cli" "$singlecall" "$url2"
echo "*** grpc calls (from $cli)"
grpcping="fortio -- grpcping -loglevel warning -n 100"
kubectl exec "$cli" "$grpcping" "$cliIp" # localhost call
kubectl exec "$cli" "$grpcping" "$srv1"
kubectl exec "$cli" "$grpcping" "$srv2"

# svc access:
echo "*** Access using service url (from $cli)"
kubectl exec "$cli" "$singlecall" "http://echosrv$debugurlsuffix"
