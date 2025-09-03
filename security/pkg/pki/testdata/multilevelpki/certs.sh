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

# This script generates all keys and certs in the 3level directory.
# There are 3 entities: root CA, intermediate CA and intermediate CA2. The certificates of the 3 CAs form a certification chain.

# Root CA
#openssl genrsa -out root-key.pem 4096
openssl req -new -key root-key.pem -out root-cert.csr -sha256 <<EOF
US
California
Sunnyvale
Istio
Test
Root CA
test@istio.io


EOF
openssl x509 -req -days 3650 -in root-cert.csr -sha256 -signkey root-key.pem -out root-cert.pem

# Intermediate CA
#openssl genrsa -out int-key.pem 4096
openssl req -new -key int-key.pem -out int-cert.csr -config int-cert.cfg -batch -sha256

openssl x509 -req -days 3650 -in int-cert.csr -sha256 -CA root-cert.pem -CAkey root-key.pem -CAcreateserial -out int-cert.pem -extensions v3_req -extfile int-cert.cfg

# Intermediate CA2
#openssl genrsa -out int2-key.pem 4096
openssl req -new -key int2-key.pem -out int2-cert.csr -config int2-cert.cfg -batch -sha256

openssl x509 -req -days 3650 -in int2-cert.csr -sha256 -CA int-cert.pem -CAkey int-key.pem -CAcreateserial -out int2-cert.pem -extensions v3_req -extfile int2-cert.cfg



cat root-cert.pem > int-cert-chain.pem
cat int-cert.pem >> int-cert-chain.pem
cp int-cert-chain.pem int2-cert-chain.pem
cat int2-cert.pem >> int2-cert-chain.pem

rm ./*csr
rm ./*srl
