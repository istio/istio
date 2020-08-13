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

WD=$(dirname "$0")
WD=$(cd "$WD" || exit; pwd)

cat > "${WD}/client.conf" <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth, serverAuth
subjectAltName = @alt_names
[alt_names]
DNS = *.example.com
EOF

cat > "${WD}/server.conf" <<EOF
[req]
req_extensions = v3_req
distinguished_name = req_distinguished_name
[req_distinguished_name]
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth, serverAuth
subjectAltName = @alt_names
[alt_names]
DNS = *.example.com
EOF

openssl req -new -newkey rsa:4096 -x509 -sha256 \
        -days 3650 -nodes -out rootA.crt -keyout rootA.key \
        -subj "/C=US/ST=Denial/L=Ether/O=Dis/CN=*.example.com" \
        -addext "subjectAltName = DNS:*.example.com"

openssl genrsa -out "clientA.key" 2048
openssl req -new -key "clientA.key" -out clientA.csr -subj "/CN=*.example.com" -config "${WD}/client.conf"
openssl x509 -req -days 3650 -CA rootA.crt -CAkey rootA.key -set_serial 0 -in clientA.csr -out clientA.crt -extensions v3_req -extfile "${WD}/client.conf"

openssl genrsa -out "serverA.key" 2048
openssl req -new -key "serverA.key" -out serverA.csr -subj "/CN=*.example.com" -config "${WD}/server.conf"
openssl x509 -req -days 3650 -CA rootA.crt -CAkey rootA.key -set_serial 0 -in serverA.csr -out serverA.crt -extensions v3_req -extfile "${WD}/server.conf"


openssl req -new -newkey rsa:4096 -x509 -sha256 \
        -days 3650 -nodes -out rootB.crt -keyout rootB.key \
        -subj "/C=US/ST=Denial/L=Ether/O=Dis/CN=*.example.com" \
        -addext "subjectAltName = DNS:*.example.com"

openssl genrsa -out "clientB.key" 2048
openssl req -new -key "clientB.key" -out clientB.csr -subj "/CN=*.example.com" -config "${WD}/client.conf"
openssl x509 -req -days 3650 -CA rootB.crt -CAkey rootB.key -set_serial 0 -in clientB.csr -out clientB.crt -extensions v3_req -extfile "${WD}/client.conf"

openssl genrsa -out "serverB.key" 2048
openssl req -new -key "serverB.key" -out serverB.csr -subj "/CN=*.example.com" -config "${WD}/server.conf"
openssl x509 -req -days 3650 -CA rootB.crt -CAkey rootB.key -set_serial 0 -in serverB.csr -out serverB.crt -extensions v3_req -extfile "${WD}/server.conf"
