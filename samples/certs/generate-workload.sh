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

set -euo pipefail

name=${1:-foo}
ns=${2:-$name}
sa=${3:-$name}
tmp=${4:-""}
san="spiffe://trust-domain-$name/ns/$ns/sa/$sa"

DIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

FINAL_DIR=$DIR
if [ -n "$tmp" ]; then
  if [ -d "$tmp" ]; then
    FINAL_DIR=$tmp
    cp "$DIR"/root-cert.pem "$FINAL_DIR"
    cp "$DIR"/ca-cert.pem "$FINAL_DIR"
    cp "$DIR"/ca-key.pem "$FINAL_DIR"
    cp "$DIR"/cert-chain.pem "$FINAL_DIR"
  else
    echo "tmp argument is not a directory: $tmp"
    exit 1
  fi
fi

function cleanup() {
  if [ -f "$FINAL_DIR"/.srl ]; then
    rm "$FINAL_DIR"/.srl
  fi
  if [ -f "$FINAL_DIR"/ca-cert.srl ]; then
    rm "$FINAL_DIR"/ca-cert.srl
  fi
  if [ -f "$FINAL_DIR"/workload.cfg ]; then
    rm "$FINAL_DIR"/workload.cfg
  fi
  if [ -f "$FINAL_DIR"/workload.csr ]; then
    rm "$FINAL_DIR"/workload.csr
  fi
}

trap cleanup EXIT

openssl genrsa -out "$FINAL_DIR/workload-$name-key.pem" 2048

cat > "$FINAL_DIR"/workload.cfg <<EOF
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

openssl req -new -key "$FINAL_DIR/workload-$name-key.pem" -subj "/" -out "$FINAL_DIR"/workload.csr -config "$FINAL_DIR"/workload.cfg

openssl x509 -req -in "$FINAL_DIR"/workload.csr -CA "$FINAL_DIR"/ca-cert.pem -CAkey "$FINAL_DIR"/ca-key.pem -CAcreateserial \
-out "$FINAL_DIR/workload-$name-cert.pem" -days 3650 -extensions v3_req -extfile "$FINAL_DIR"/workload.cfg

cat "$FINAL_DIR"/cert-chain.pem >> "$FINAL_DIR/workload-$name-cert.pem"

echo "Generated workload-$name-[cert|key].pem with URI SAN $san"
openssl verify -CAfile <(cat "$FINAL_DIR"/cert-chain.pem "$FINAL_DIR"/root-cert.pem) "$FINAL_DIR/workload-$name-cert.pem"

