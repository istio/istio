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

openssl genrsa -out root.key 2048
openssl req -x509 -new -nodes -key root.key -sha256 -days 1825 -out root.cert

# Server certificate:
openssl genrsa -out server-key.cert 2048
openssl req -new -key server-key.cert -out server.csr
openssl x509 -req -in server.csr  -CA root.cert -CAkey root.key -out server.cert -days 1650 -sha256 -extfile server_ext.cnf

# Client certificate:
openssl genrsa -out client-key.cert 2048
openssl req -new -key client-key.cert -out client.csr
openssl x509 -req -in client.csr  -CA root.cert -CAkey root.key -out client.cert -days 1650 -sha256 -extfile client_ext.cnf


# Stackdriver certs need localhost as the common name
