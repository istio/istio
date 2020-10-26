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
ns=${2:-$name}
sa=${3:-$name}
dir=${4:-"."}
tmp=${5:-""}
san="spiffe://trust-domain-$name/ns/$ns/sa/$sa"

if [ ! -d "$dir/$tmp" ]; then
  mkdir "$dir/$tmp"
fi

openssl genrsa -out "$dir/$tmp/workload-$name-key.pem" 2048

cat > "$dir"/workload.cfg <<EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
x509_extensions = v3_req
prompt = no
[req_distinguished_name]
countryName = US
[v3_req]
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
basicConstraints = critical, CA:FALSE
subjectAltName = critical, @alt_names
[alt_names]
URI = $san
EOF

openssl req -new -key "$dir/$tmp/workload-$name-key.pem" -subj "/" -out "$dir"/workload.csr -config "$dir"/workload.cfg

openssl x509 -req -in "$dir"/workload.csr -CA "$dir"/ca-cert.pem -CAkey "$dir"/ca-key.pem -CAcreateserial \
-out "$dir/$tmp/workload-$name-cert.pem" -days 3650 -extensions v3_req -extfile "$dir"/workload.cfg

cat "$dir"/cert-chain.pem >> "$dir/$tmp/workload-$name-cert.pem"

echo "Generated workload-$name-[cert|key].pem with URI SAN $san"
openssl verify -CAfile <(cat "$dir"/cert-chain.pem "$dir"/root-cert.pem) "$dir/$tmp/workload-$name-cert.pem"

# clean temporary files
if [ -f "$dir"/.srl ]; then
  rm "$dir"/.srl
fi
if [ -f "$dir"/ca-cert.srl ]; then
  rm "$dir"/ca-cert.srl
fi
rm "$dir"/workload.cfg "$dir"/workload.csr
