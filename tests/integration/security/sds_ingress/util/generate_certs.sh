#!/bin/sh

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

openssl req -new -newkey rsa:4096 -x509 -sha256 \
        -days 3650 -nodes -out rootA.crt -keyout rootA.key \
        -subj "/C=US/ST=Denial/L=Ether/O=Dis/CN=*.example.com" \
        -addext "subjectAltName = DNS:*.example.com"

openssl req -out clientA.csr -newkey rsa:2048 -nodes -keyout clientA.key -subj "/CN=*.example.com/O= A organization"
openssl x509 -req -days 3650 -CA rootA.crt -CAkey rootA.key -set_serial 0 -in clientA.csr -out clientA.crt

openssl req -out serverA.csr -newkey rsa:2048 -nodes -keyout serverA.key -subj "/CN=*.example.com/O= A organization"
openssl x509 -req -days 3650 -CA rootA.crt -CAkey rootA.key -set_serial 0 -in serverA.csr -out serverA.crt


openssl req -new -newkey rsa:4096 -x509 -sha256 \
        -days 3650 -nodes -out rootB.crt -keyout rootB.key \
        -subj "/C=US/ST=Denial/L=Ether/O=Dis/CN=*.example.com" \
        -addext "subjectAltName = DNS:*.example.com"

openssl req -out clientB.csr -newkey rsa:2048 -nodes -keyout clientB.key -subj "/CN=*.example.com/O= B organization"
openssl x509 -req -days 3650 -CA rootB.crt -CAkey rootB.key -set_serial 0 -in clientB.csr -out clientB.crt

openssl req -out serverB.csr -newkey rsa:2048 -nodes -keyout serverB.key -subj "/CN=*.example.com/O= A organization"
openssl x509 -req -days 3650 -CA rootB.crt -CAkey rootB.key -set_serial 0 -in serverB.csr -out serverB.crt
