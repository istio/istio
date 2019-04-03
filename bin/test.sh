#!/bin/bash -e

# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

ISTIO_PATH="$1"
if [ -z "$ISTIO_PATH"]; then
    echo "Usage: test.sh <istio-directory>"
    exit 1
fi

set -x
cd $ISTIO_PATH

kubectl label namespace default istio-env=istio-control --overwrite

kubectl delete -f samples/bookinfo/platform/kube/bookinfo.yaml --ignore-not-found
kubectl delete -f samples/bookinfo/networking/destination-rule-all-mtls.yaml --ignore-not-found
kubectl delete -f samples/bookinfo/networking/bookinfo-gateway.yaml --ignore-not-found

kubectl apply -f samples/bookinfo/platform/kube/bookinfo.yaml
kubectl apply -f samples/bookinfo/networking/destination-rule-all-mtls.yaml
kubectl apply -f samples/bookinfo/networking/bookinfo-gateway.yaml

kubectl rollout status deployments productpage-v1 --timeout=$WAIT_TIMEOUT
kubectl get pod

export INGRESS_HOST=$(kubectl -n istio-ingress get service ingressgateway -o jsonpath='{.spec.clusterIP}')
#export INGRESS_HOST=$(kubectl -n istio-ingress get service ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
export INGRESS_PORT=$(kubectl -n istio-ingress get service ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].port}')
export SECURE_INGRESS_PORT=$(kubectl -n istio-ingress get service ingressgateway -o jsonpath='{.spec.ports[?(@.name=="https")].port}')
export GATEWAY_URL=$INGRESS_HOST:$INGRESS_PORT
set +e
n=1
while true
do
    RESULT=$(curl -s -o /dev/null -w "%{http_code}" http://${GATEWAY_URL}/productpage)
    if [ $RESULT -eq "200"  ]; then
        break
    fi
    if [ $n -ge 5 ]; then
        exit 1
    fi
    n=$((n+1))
    echo "Retrying in 10s..."
    sleep 10
done
set -e

echo "Cleaning up..."
kubectl delete -f samples/bookinfo/platform/kube/bookinfo.yaml --ignore-not-found
kubectl delete -f samples/bookinfo/networking/destination-rule-all-mtls.yaml --ignore-not-found
kubectl delete -f samples/bookinfo/networking/bookinfo-gateway.yaml --ignore-not-found
