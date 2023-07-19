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
touch "${WD}/index.txt"

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

cat > "${WD}/crlA.conf" <<EOF
[ ca ]
default_ca      = CA_default            # The default ca section

[ CA_default ]
dir             = "${WD}"         # Where everything is kept
database        = "${WD}/index.txt"    # database index file.
certificate     = "${WD}/rootA.crt"   # The CA certificate
private_key     = "${WD}/rootA.key"    # The private key

# crlnumber must also be commented out to leave a V1 CRL.
crl_extensions = crl_ext

default_md      = sha256                # use SHA-256 by default
default_crl_days= 3650                  # how long before next CRL

[ crl_ext ]
# CRL extensions.
# Only issuerAltName and authorityKeyIdentifier make any sense in a CRL.
authorityKeyIdentifier=keyid:always
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
        -days 3650 -nodes -out "${WD}/rootA.crt" -keyout "${WD}/rootA.key" \
        -subj "/C=US/ST=Denial/L=Ether/O=Dis/CN=*.example.com" \
        -addext "subjectAltName = DNS:*.example.com"

openssl genrsa -out "${WD}/clientA.key" 2048
openssl req -new -key "${WD}/clientA.key" -out "${WD}/clientA.csr" -subj "/CN=*.example.com" -config "${WD}/client.conf"
openssl x509 -req -days 3650 -CA "${WD}/rootA.crt" -CAkey "${WD}/rootA.key" -set_serial 0 -in "${WD}/clientA.csr" -out "${WD}/clientA.crt" -extensions v3_req -extfile "${WD}/client.conf"

openssl genrsa -out "${WD}/serverA.key" 2048
openssl req -new -key "${WD}/serverA.key" -out "${WD}/serverA.csr" -subj "/CN=*.example.com" -config "${WD}/server.conf"
openssl x509 -req -days 3650 -CA "${WD}/rootA.crt" -CAkey "${WD}/rootA.key" -set_serial 0 -in "${WD}/serverA.csr" -out "${WD}/serverA.crt" -extensions v3_req -extfile "${WD}/server.conf"


openssl req -new -newkey rsa:4096 -x509 -sha256 \
        -days 3650 -nodes -out "${WD}/rootB.crt" -keyout "${WD}/rootB.key" \
        -subj "/C=US/ST=Denial/L=Ether/O=Dis/CN=*.example.com" \
        -addext "subjectAltName = DNS:*.example.com"

openssl genrsa -out "${WD}/clientB.key" 2048
openssl req -new -key "${WD}/clientB.key" -out "${WD}/clientB.csr" -subj "/CN=*.example.com" -config "${WD}/client.conf"
openssl x509 -req -days 3650 -CA "${WD}/rootB.crt" -CAkey "${WD}/rootB.key" -set_serial 0 -in "${WD}/clientB.csr" -out "${WD}/clientB.crt" -extensions v3_req -extfile "${WD}/client.conf"

openssl genrsa -out "${WD}/serverB.key" 2048
openssl req -new -key "${WD}/serverB.key" -out "${WD}/serverB.csr" -subj "/CN=*.example.com" -config "${WD}/server.conf"
openssl x509 -req -days 3650 -CA "${WD}/rootB.crt" -CAkey "${WD}/rootB.key" -set_serial 0 -in "${WD}/serverB.csr" -out "${WD}/serverB.crt" -extensions v3_req -extfile "${WD}/server.conf"

# revoke one of the client certificates for CRL testing purpose
openssl ca -config "${WD}/crlA.conf" -revoke "${WD}/clientA.crt"
openssl ca -gencrl -out "${WD}/rootA.crl" -config "${WD}/crlA.conf"

# remove the database entry for the previous revoked certificate, so that we can generate a new dummy CRL entry for an unused client cert, to be used for integration tests
cat /dev/null > "${WD}/index.txt"
openssl genrsa -out "${WD}/clientA1.key" 2048
openssl req -new -key "${WD}/clientA1.key" -out "${WD}/clientA1.csr" -subj "/CN=*.example.com" -config "${WD}/client.conf"
openssl x509 -req -days 3650 -CA "${WD}/rootA.crt" -CAkey "${WD}/rootA.key" -set_serial 1 -in "${WD}/clientA1.csr" -out "${WD}/clientA1.crt" -extensions v3_req -extfile "${WD}/client.conf"

openssl ca -config "${WD}/crlA.conf" -revoke "${WD}/clientA1.crt"
openssl ca -gencrl -out "${WD}/dummyA.crl" -config "${WD}/crlA.conf"
