#!/bin/bash
#
# Copyright Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

name=${1:-foo}
san="spiffe://trust-domain-$name/ns/$name/sa/$name"

openssl genrsa -out "workload-$name-key.pem" 2048

cat > workload.cfg <<EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
x509_extensions = v3_req
prompt = no
[req_distinguished_name]
[v3_req]
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
basicConstraints = critical, CA:FALSE
subjectAltName = critical, @alt_names
[alt_names]
URI = $san
EOF

openssl req -new -key "workload-$name-key.pem" -subj "/" -out workload.csr -config workload.cfg

openssl x509 -req -in workload.csr -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial \
-out "workload-$name-cert.pem" -days 3650 -extensions v3_req -extfile workload.cfg

cat cert-chain.pem >> "workload-$name-cert.pem"

echo "Generated workload-$name-[cert|key].pem with URI SAN $san"
openssl verify -CAfile <(cat cert-chain.pem root-cert.pem) "workload-$name-cert.pem"

# clean temporary files
rm ca-cert.srl workload.cfg workload.csr
