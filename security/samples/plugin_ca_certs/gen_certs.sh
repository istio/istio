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
openssl req -newkey rsa:2048 -nodes -keyout root-key-alt.pem -x509 -days 36500 -out root-cert-alt.pem <<EOF
US
California
Sunnyvale
Istio
Test
Root CA
testrootca@istio.io
EOF

for suffix in "" "-alt" "-alt-2"; do
  rootSuffix="${suffix}"
  if [[ "${suffix}" == "-alt-2" ]]; then
    rootSuffix="-alt"
  fi
  echo 'Generate private key for Istio CA.'
  openssl genrsa -out "ca-key${suffix}.pem" 2048

  echo 'Generate CSR for Istio CA.'
  openssl req -new -key "ca-key${suffix}.pem" -out ca-cert.csr -config ca.cfg -batch -sha256

  echo 'Sign the cert for Istio CA.'
  openssl x509 -req -days 36500 -in ca-cert.csr -sha256 -CA "root-cert${rootSuffix}.pem" -CAkey "root-key${rootSuffix}.pem" -CAcreateserial -out "ca-cert${suffix}.pem" -extensions v3_req -extfile ca.cfg

  echo 'Generate cert chain file.'
  cp "ca-cert${suffix}.pem" "cert-chain${suffix}.pem"
done

rm ./*csr
rm ./*srl
rm root-key.pem
rm root-key-alt.pem

cat root-cert.pem root-cert-alt.pem > root-cert-combined.pem
mv ./*.pem ../../../samples/certs/
