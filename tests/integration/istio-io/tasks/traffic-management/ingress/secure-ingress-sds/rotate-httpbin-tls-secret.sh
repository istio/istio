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

echo "Delete the gateway’s secret and create a new one to change the ingress gateway’s credentials."

kubectl -n istio-system delete secret httpbin-credential

sh ./mtls-go-example.sh "httpbin.new.example.com"

kubectl create -n istio-system secret generic httpbin-credential \
--from-file=key=httpbin.new.example.com/3_application/private/httpbin.example.com.key.pem \
--from-file=cert=httpbin.new.example.com/3_application/certs/httpbin.example.com.cert.pem

kubectl get -n istio-system secret httpbin-credential