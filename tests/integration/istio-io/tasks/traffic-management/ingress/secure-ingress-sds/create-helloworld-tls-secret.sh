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

sh ./mtls-go-example.sh "helloworld-v1.example.com" "helloworld-v1.example.com"

kubectl create -n istio-system secret generic helloworld-credential \
--from-file=key=helloworld-v1.example.com/3_application/private/helloworld-v1.example.com.key.pem \
--from-file=cert=helloworld-v1.example.com/3_application/certs/helloworld-v1.example.com.cert.pem

kubectl get -n istio-system secret helloworld-credential