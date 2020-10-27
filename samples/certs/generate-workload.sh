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
tmp=${4:-""}
san="spiffe://trust-domain-$name/ns/$ns/sa/$sa"

DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

if [ ! -d "$DIR/$tmp" ]; then
  mkdir "$DIR/$tmp"
fi

openssl genrsa -out "$DIR/$tmp/workload-$name-key.pem" 2048

cat > "$DIR"/workload.cfg <<EOF
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

openssl req -new -key "$DIR/$tmp/workload-$name-key.pem" -subj "/" -out "$DIR"/workload.csr -config "$DIR"/workload.cfg

openssl x509 -req -in "$DIR"/workload.csr -CA "$DIR"/ca-cert.pem -CAkey "$DIR"/ca-key.pem -CAcreateserial \
-out "$DIR/$tmp/workload-$name-cert.pem" -days 3650 -extensions v3_req -extfile "$DIR"/workload.cfg

cat "$DIR"/cert-chain.pem >> "$DIR/$tmp/workload-$name-cert.pem"

echo "Generated workload-$name-[cert|key].pem with URI SAN $san"
openssl verify -CAfile <(cat "$DIR"/cert-chain.pem "$DIR"/root-cert.pem) "$DIR/$tmp/workload-$name-cert.pem"

# clean temporary files
if [ -f "$DIR"/.srl ]; then
  rm "$DIR"/.srl
fi
if [ -f "$DIR"/ca-cert.srl ]; then
  rm "$DIR"/ca-cert.srl
fi
rm "$DIR"/workload.cfg "$DIR"/workload.csr
