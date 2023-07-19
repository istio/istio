#!/bin/bash

# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http:#www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Generates certificates used for testing
# We generate a cert for a workload (ns=default, sa=default) and control plane
WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

set -ex

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
URI = spiffe://cluster.local/ns/default/sa/default
EOF

cat > "${WD}/dns-client.conf" <<EOF
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
DNS = server.default.svc
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
URI = spiffe://cluster.local/ns/istio-system/sa/istio-pilot-service-account
DNS.1 = istiod.istio-system
DNS.2 = istiod.istio-system.svc
DNS.3 = istio-pilot.istio-system
DNS.4 = istio-pilot.istio-system.svc
DNS.5 = localhost
EOF


cat > "${WD}/mountedcerts-server.conf" <<EOF
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
URI = spiffe://cluster.local/ns/mounted-certs/sa/server
DNS = server.mounted-certs.svc
EOF

cat > "${WD}/mountedcerts-client.conf" <<EOF
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
URI = spiffe://cluster.local/ns/mounted-certs/sa/client
DNS = client.mounted-certs.svc
EOF

cat > "${WD}/crl.conf" <<EOF
[ ca ]
default_ca      = CA_default            # The default ca section

[ CA_default ]
dir             = "${WD}"         # Where everything is kept
database        = "${WD}/index.txt"    # database index file.
certificate     = "${WD}/pilot/ca-cert.pem"    # The CA certificate
private_key     = "${WD}/pilot/ca-key.pem"    # The private key

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
DNS = cluster.local
EOF

# Create a certificate authority
openssl genrsa -out "${WD}/pilot/ca-key.pem" 2048
openssl req -x509 -new -nodes -key "${WD}/pilot/ca-key.pem" -days 100000 -out "${WD}/pilot/root-cert.pem" -subj "/CN=cluster.local"
cp "${WD}/pilot/root-cert.pem" "${WD}/default/root-cert.pem"
cp "${WD}/pilot/root-cert.pem" "${WD}/dns/root-cert.pem"
cp "${WD}/pilot/root-cert.pem" "${WD}/pilot/ca-cert.pem"
cp "${WD}/pilot/root-cert.pem" "${WD}/mountedcerts-server/root-cert.pem"
cp "${WD}/pilot/root-cert.pem" "${WD}/mountedcerts-client/root-cert.pem"

# Create a server certificate
openssl genrsa -out "${WD}/pilot/key.pem" 2048
openssl req -new -sha256 -key "${WD}/pilot/key.pem" -out "${WD}/server.csr" -subj "/CN=istiod.istio-system.svc.cluster.local" -config "${WD}/server.conf"
openssl x509 -req -in "${WD}/server.csr" -CA "${WD}/pilot/root-cert.pem" -CAkey "${WD}/pilot/ca-key.pem" -CAcreateserial -out "${WD}/pilot/cert-chain.pem"  -days 100000 -extensions v3_req -extfile "${WD}/server.conf"

# Create a client certificate
openssl genrsa -out "${WD}/default/key.pem" 2048
openssl req -new -sha256 -key "${WD}/default/key.pem" -out "${WD}/client.csr" -subj "/CN=default.default.svc.cluster.local" -config "${WD}/client.conf"
openssl x509 -req -in "${WD}/client.csr" -CA "${WD}/pilot/root-cert.pem" -CAkey "${WD}/pilot/ca-key.pem" -CAcreateserial -out "${WD}/default/cert-chain.pem" -days 100000 -extensions v3_req -extfile "${WD}/client.conf"

# Create a DNS client certificate
openssl genrsa -out "${WD}/dns/key.pem" 2048
openssl req -new -sha256 -key "${WD}/dns/key.pem" -out "${WD}/dns-client.csr" -subj "/CN=server.default.svc.cluster.local" -config "${WD}/dns-client.conf"
openssl x509 -req -in "${WD}/dns-client.csr" -CA "${WD}/pilot/root-cert.pem" -CAkey "${WD}/pilot/ca-key.pem" -CAcreateserial -out "${WD}/dns/cert-chain.pem" -days 100000 -extensions v3_req -extfile "${WD}/dns-client.conf"

# Create a server certificate for MountedCerts test
openssl genrsa -out "${WD}/mountedcerts-server/key.pem" 2048
openssl req -new -sha256 -key "${WD}/mountedcerts-server/key.pem" -out "${WD}/mountedcerts-server.csr" -subj "/CN=server.mounted-certs.svc.cluster.local" -config "${WD}/mountedcerts-server.conf"
openssl x509 -req -in "${WD}/mountedcerts-server.csr" -CA "${WD}/pilot/root-cert.pem" -CAkey "${WD}/pilot/ca-key.pem" -CAcreateserial -out "${WD}/mountedcerts-server/cert-chain.pem" -days 100000 -extensions v3_req -extfile "${WD}/mountedcerts-server.conf"

# Create a client certificate for MountedCerts test
openssl genrsa -out "${WD}/mountedcerts-client/key.pem" 2048
openssl req -new -sha256 -key "${WD}/mountedcerts-client/key.pem" -out "${WD}/mountedcerts-client.csr" -subj "/CN=client.mounted-certs.svc.cluster.local" -config "${WD}/mountedcerts-client.conf"
openssl x509 -req -in "${WD}/mountedcerts-client.csr" -CA "${WD}/pilot/root-cert.pem" -CAkey "${WD}/pilot/ca-key.pem" -CAcreateserial -out "${WD}/mountedcerts-client/cert-chain.pem" -days 100000 -extensions v3_req -extfile "${WD}/mountedcerts-client.conf"

# revoke one of the server certificates for CRL testing purpose
openssl ca -config "${WD}/crl.conf" -revoke "${WD}/dns/cert-chain.pem"
openssl ca -gencrl -out "${WD}/ca.crl" -config "${WD}/crl.conf"

# remove the database entry for the previous revoked certificate, so that we can generate a new dummy CRL entry for an unused server cert,
# to be used for integration tests
cat /dev/null > "${WD}/index.txt"

openssl x509 -req -in "${WD}/server.csr" -CA "${WD}/pilot/root-cert.pem" -CAkey "${WD}/pilot/ca-key.pem" -CAcreateserial -out "${WD}/dns/cert-chain-unused.pem"  -days 100000 -extensions v3_req -extfile "${WD}/server.conf"

openssl ca -config "${WD}/crl.conf" -revoke "${WD}/dns/cert-chain-unused.pem"
openssl ca -gencrl -out "${WD}/dummy.crl" -config "${WD}/crl.conf"

rm "${WD}/server.conf" "${WD}/client.conf" "${WD}/dns-client.conf" "${WD}/crl.conf"
rm "${WD}/server.csr" "${WD}/client.csr" "${WD}/dns-client.csr"
rm "${WD}/mountedcerts-server.conf" "${WD}/mountedcerts-server.csr"
rm "${WD}/mountedcerts-client.conf" "${WD}/mountedcerts-client.csr"
rm "${WD}"/index.txt*
