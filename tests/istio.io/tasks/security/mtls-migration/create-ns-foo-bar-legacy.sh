#!/bin/bash

# Copyright Istio Authors
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

kubectl create ns foo
kubectl apply -f <(istioctl kube-inject -f @samples/httpbin/httpbin.yaml@) -n foo
kubectl apply -f <(istioctl kube-inject -f @samples/sleep/sleep.yaml@) -n foo
kubectl create ns bar
kubectl apply -f <(istioctl kube-inject -f @samples/httpbin/httpbin.yaml@) -n bar
kubectl apply -f <(istioctl kube-inject -f @samples/sleep/sleep.yaml@) -n bar
kubectl create ns legacy
kubectl apply -f @samples/sleep/sleep.yaml@ -n legacy
