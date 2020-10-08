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


# ----------------------
# Custom CA certs
# ----------------------
openssl genrsa -out custom-certs/root-key.pem 2048
openssl req -new -key custom-certs/root-key.pem -out custom-certs/root.csr -sha256 <<EOF
US
California
Sunnyvale
Istio
Test
Root CA
test@istio.io


EOF
openssl x509 -req -days 365000 -in custom-certs/root.csr -sha256 -signkey custom-certs/root-key.pem -out custom-certs/root-cert.pem

# Server TLS
openssl genrsa -out custom-certs/server-key.pem 2048
openssl req -new -key custom-certs/server-key.pem -out custom-certs/server.csr -config custom.cfg -batch -sha256
openssl x509 -req -days 365000 -in custom-certs/server.csr -sha256 -CA custom-certs/root-cert.pem -CAkey custom-certs/root-key.pem -CAcreateserial -out custom-certs/server-cert.pem -extensions v3_req -extfile custom.cfg

# Client TLS
openssl genrsa -out custom-certs/client-key.pem 2048
openssl req -new -key custom-certs/client-key.pem -out custom-certs/client.csr -config custom.cfg -batch -sha256
openssl x509 -req -days 365000 -in custom-certs/client.csr -sha256 -CA custom-certs/root-cert.pem -CAkey custom-certs/root-key.pem -CAcreateserial -out custom-certs/client-cert.pem -extensions v3_req -extfile custom.cfg

# Workload Cert
openssl genrsa -out custom-certs/workload-key.pem 2048
openssl req -new -key custom-certs/workload-key.pem -out custom-certs/workload-cert.csr -config custom.cfg -batch -sha256
openssl x509 -req -days 365000 -in custom-certs/workload-cert.csr -sha256 -CA custom-certs/root-cert.pem -CAkey custom-certs/root-key.pem -CAcreateserial -out custom-certs/workload-cert.pem -extensions v3_req -extfile custom.cfg

cat custom-certs/root-cert.pem > custom-certs/workload-cert-chain.pem
cat custom-certs/workload-cert.pem >> custom-certs/workload-cert-chain.pem

# ----------------------
# Self generate certs
# ----------------------

openssl genrsa -out istio-certs/root-key.pem 2048
openssl req -new -key istio-certs/root-key.pem -out istio-certs/root-cert.csr -sha256 <<EOF
US
California
Sunnyvale
Istio
Test
Root CA
test@istio.io


EOF
openssl x509 -req -days 365000 -in istio-certs/root-cert.csr -sha256 -signkey istio-certs/root-key.pem -out istio-certs/root-cert.pem

# Workload certs by Istio
openssl genrsa -out istio-certs/workload-key.pem 2048
openssl req -new -key istio-certs/workload-key.pem -out istio-certs/workload-cert.csr -config istio.cfg -batch -sha256
openssl x509 -req -days 365000 -in istio-certs/workload-cert.csr -sha256 -CA istio-certs/root-cert.pem -CAkey istio-certs/root-key.pem -CAcreateserial -out istio-certs/workload-cert.pem -extensions v3_req -extfile istio.cfg


# ----------------------
# Self generate ECC certs
# ----------------------
openssl ecparam -genkey -name prime256v1 -out istio-ecc-certs/root-key.pem -noout
openssl req -new -key istio-ecc-certs/root-key.pem -out istio-ecc-certs/root.csr -sha256 <<EOF
US
California
Sunnyvale
Istio
Test
Root CA
test@istio.io


EOF
openssl x509 -req -days 365000 -in istio-ecc-certs/root.csr -sha256 -signkey istio-ecc-certs/root-key.pem -out istio-ecc-certs/root-cert.pem

# Workload Cert
openssl genrsa -out istio-ecc-certs/workload-key.pem 2048
openssl req -new -key istio-ecc-certs/workload-key.pem -out istio-ecc-certs/workload.csr -config istio.cfg -batch -sha256
openssl x509 -req -days 365000 -in istio-ecc-certs/workload.csr -sha256 -CA istio-ecc-certs/root-cert.pem -CAkey istio-ecc-certs/root-key.pem -CAcreateserial -out istio-ecc-certs/workload-cert.pem -extensions v3_req -extfile istio.cfg



# ----------------------
# Custom External CA with ECC certificates
# ----------------------
openssl ecparam -genkey -name prime256v1 -out custom-ecc-certs/root-key.pem -noout
openssl req -new -key custom-ecc-certs/root-key.pem -out custom-ecc-certs/root.csr -sha256 <<EOF
US
California
Sunnyvale
Istio
Test
ECC Root CA
test@istio.io


EOF
openssl x509 -req -days 365000 -in custom-ecc-certs/root.csr -sha256 -signkey custom-ecc-certs/root-key.pem -out custom-ecc-certs/root-cert.pem

# Server TLS
openssl ecparam -genkey -name prime256v1 -out custom-ecc-certs/server-key.pem -noout
openssl req -new -key custom-ecc-certs/server-key.pem -out custom-ecc-certs/server.csr -config custom.cfg -batch -sha256
openssl x509 -req -days 365000 -in custom-ecc-certs/server.csr -sha256 -CA custom-ecc-certs/root-cert.pem -CAkey custom-ecc-certs/root-key.pem -CAcreateserial -out custom-ecc-certs/server-cert.pem -extensions v3_req -extfile custom.cfg

# Client TLS
openssl ecparam -genkey -name prime256v1 -out custom-ecc-certs/client-key.pem -noout
openssl req -new -key custom-ecc-certs/client-key.pem -out custom-ecc-certs/client.csr -config custom.cfg -batch -sha256
openssl x509 -req -days 365000 -in custom-ecc-certs/client.csr -sha256 -CA custom-ecc-certs/root-cert.pem -CAkey custom-ecc-certs/root-key.pem -CAcreateserial -out custom-ecc-certs/client-cert.pem -extensions v3_req -extfile custom.cfg

# Workload Cert
openssl ecparam -genkey -name prime256v1 -out custom-ecc-certs/workload-key.pem -noout
openssl req -new -key custom-ecc-certs/workload-key.pem -out custom-ecc-certs/workload-cert.csr -config custom.cfg -batch -sha256
openssl x509 -req -days 365000 -in custom-ecc-certs/workload-cert.csr -sha256 -CA custom-ecc-certs/root-cert.pem -CAkey custom-ecc-certs/root-key.pem -CAcreateserial -out custom-ecc-certs/workload-cert.pem -extensions v3_req -extfile custom.cfg

cat custom-ecc-certs/root-cert.pem > custom-ecc-certs/workload-cert-chain.pem
cat custom-ecc-certs/workload-cert.pem >> custom-ecc-certs/workload-cert-chain.pem

# -------------
# MIXING ROOT
# --------------

cat custom-certs/root-cert.pem  > mixing-custom-istio-root.pem
cat istio-certs/root-cert.pem >> mixing-custom-istio-root.pem

cat custom-certs/root-cert.pem  > mixing-custom-ecc-root.pem
cat istio-certs/root-cert.pem >> mixing-custom-ecc-root.pem

rm ./**/*.csr
rm ./**/*.srl
