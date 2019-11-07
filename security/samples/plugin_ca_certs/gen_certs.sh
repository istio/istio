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

echo 'Generate key and cert for root CA.'
openssl req -newkey rsa:2048 -nodes -keyout root-key.pem -x509 -days 36500 -out root-cert.pem <<EOF
US
California
Sunnyvale
Istio
Test
Root CA
testrootca@istio.io


EOF

echo 'Generate private key for Istio CA.'
openssl genrsa -out ca-key.pem 2048

echo 'Generate CSR for Istio CA.'
openssl req -new -key ca-key.pem -out ca-cert.csr -config ca.cfg -batch -sha256

echo 'Sign the cert for Istio CA.'
openssl x509 -req -days 36500 -in ca-cert.csr -sha256 -CA root-cert.pem -CAkey root-key.pem -CAcreateserial -out ca-cert.pem -extensions v3_req -extfile ca.cfg

rm ./*csr
rm ./*srl

echo 'Generate cert chain file.'
cp ca-cert.pem cert-chain.pem

mv ./*.pem ../../../samples/certs/
