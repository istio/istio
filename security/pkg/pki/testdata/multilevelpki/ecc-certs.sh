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
#openssl ecparam -genkey -name prime256v1 -out ecc-root-key.pem -noout
openssl req -new -key ecc-root-key.pem -out ecc-root-cert.csr -sha256 <<EOF
US
California
Sunnyvale
Istio
Test
Root CA
test@istio.io


EOF
openssl x509 -req -days 3650 -in ecc-root-cert.csr -sha256 -signkey ecc-root-key.pem -out ecc-root-cert.pem

# Intermediate CA
#openssl ecparam -genkey -name prime256v1 -out ecc-int-key.pem -noout
openssl req -new -key ecc-int-key.pem -out ecc-int-cert.csr -config int-cert.cfg -batch -sha256

openssl x509 -req -days 3650 -in ecc-int-cert.csr -sha256 -CA ecc-root-cert.pem -CAkey ecc-root-key.pem -CAcreateserial -out ecc-int-cert.pem -extensions v3_req -extfile int-cert.cfg


# Intermediate CA2
#openssl ecparam -genkey -name prime256v1 -out ecc-int2-key.pem -noout
openssl req -new -key ecc-int2-key.pem -out ecc-int2-cert.csr -config int2-cert.cfg -batch -sha256

openssl x509 -req -days 3650 -in ecc-int2-cert.csr -sha256 -CA ecc-int-cert.pem -CAkey ecc-int-key.pem -CAcreateserial -out ecc-int2-cert.pem -extensions v3_req -extfile int2-cert.cfg

cat ecc-root-cert.pem > ecc-int-cert-chain.pem
cat ecc-int-cert.pem >> ecc-int-cert-chain.pem
cp ecc-int-cert-chain.pem ecc-int2-cert-chain.pem
cat ecc-int2-cert.pem >> ecc-int2-cert-chain.pem

rm ./*csr
rm ./*srl
