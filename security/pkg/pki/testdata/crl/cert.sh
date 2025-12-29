#!/usr/bin/env bash

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

# This script generates a root and intermediate certificates, along with their CRLs.
# In case of certificate expiration, execute this script from this directory to regenerate the necessary files.

set -euo pipefail

echo "🔄 Cleaning up previous files..."
rm -rf root-cert.pem ca-cert.pem ca-key.pem ca-crl.pem

mkdir -p root/newcerts root/crl intermediate/newcerts intermediate/crl certs

echo "📝 Initializing CA DBs..."

# Root CA DB setup
touch root/index.txt
echo 1000 > root/serial
echo 1000 > root/crlnumber

# Intermediate CA DB setup
touch intermediate/index.txt
echo 2000 > intermediate/serial
echo 2000 > intermediate/crlnumber

#####################
# Root OpenSSL Config
#####################
cat > root/root-openssl.cnf <<EOF
[ ca ]
default_ca = CA_default

[ CA_default ]
dir               = ./root
certs             = \$dir
new_certs_dir     = \$dir/newcerts
database          = \$dir/index.txt
serial            = \$dir/serial
crlnumber         = \$dir/crlnumber
RANDFILE          = \$dir/.rand
private_key       = \$dir/root-key.pem
certificate       = \$dir/root-cert.pem
default_days      = 3650
default_md        = sha256
default_crl_days  = 30
policy            = policy_strict
x509_extensions   = v3_ca
crl_extensions    = crl_ext

[ policy_strict ]
commonName        = supplied

[ req ]
default_bits        = 4096
default_md          = sha256
distinguished_name  = req_distinguished_name
x509_extensions     = v3_ca
string_mask         = utf8only
prompt              = no

[ req_distinguished_name ]
commonName         = Root CA

[ v3_ca ]
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always,issuer
basicConstraints = critical, CA:true
keyUsage = critical, digitalSignature, cRLSign, keyCertSign

[ crl_ext ]
authorityKeyIdentifier = keyid:always
EOF

#############################
# Intermediate OpenSSL Config
#############################
cat > intermediate/ca-openssl.cnf <<EOF
[ ca ]
default_ca = CA_default

[ CA_default ]
dir               = ./intermediate
certs             = \$dir
new_certs_dir     = \$dir/newcerts
database          = \$dir/index.txt
serial            = \$dir/serial
crlnumber         = \$dir/crlnumber
RANDFILE          = \$dir/.rand
private_key       = \$dir/ca-key.pem
certificate       = \$dir/ca-cert.pem
default_days      = 3650
default_md        = sha256
default_crl_days  = 30
policy            = policy_strict
x509_extensions   = v3_intermediate_ca
crl_extensions    = crl_ext

[ policy_strict ]
commonName        = supplied

[ req ]
default_bits        = 4096
default_md          = sha256
distinguished_name  = req_distinguished_name
x509_extensions     = v3_intermediate_ca
string_mask         = utf8only
prompt              = no

[ req_distinguished_name ]
commonName         = Intermediate CA

[ v3_intermediate_ca ]
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always,issuer
basicConstraints = critical, CA:true, pathlen:0
keyUsage = critical, digitalSignature, cRLSign, keyCertSign

[ crl_ext ]
authorityKeyIdentifier = keyid:always
EOF

#####################
# Generate Root Certs
#####################
echo "🔐 Generating Root CA key and cert..."
openssl genrsa -out root/root-key.pem 4096
openssl req -config root/root-openssl.cnf -key root/root-key.pem -new -x509 -days 3650 -sha256 -out root/root-cert.pem

###############################
# Generate Intermediate CSR/Cert
###############################
echo "📄 Creating Intermediate CSR..."
openssl genrsa -out intermediate/ca-key.pem 4096
openssl req -config intermediate/ca-openssl.cnf -new -key intermediate/ca-key.pem -out intermediate/ca.csr

echo "📝 Signing Intermediate CSR with Root CA..."
openssl ca -batch -config root/root-openssl.cnf -extensions v3_ca -days 3650 -notext -md sha256 \
    -in intermediate/ca.csr -out intermediate/ca-cert.pem

#####################
# Generate Initial CRLs
#####################
echo "📚 Generating initial CRLs..."
openssl ca -gencrl -config root/root-openssl.cnf -out root/crl/root.crl.pem
openssl ca -gencrl -config intermediate/ca-openssl.cnf -out intermediate/crl/intermediate.crl.pem

echo "📎 Combining Root and Intermediate CRLs..."
cat root/crl/root.crl.pem intermediate/crl/intermediate.crl.pem > ca-crl.pem

echo "🔗 Creating cert chain..."
cat intermediate/ca-cert.pem root/root-cert.pem > cert-chain.pem

##################################################
# Collect required artifacts & final clean-up
##################################################
echo "📦 Collecting artifacts..."
cp root/root-cert.pem  ./root-cert.pem
cp intermediate/ca-cert.pem ./ca-cert.pem
cp intermediate/ca-key.pem  ./ca-key.pem
# ca-crl.pem is already in place

echo "🧹 Removing intermediate files..."
rm -rf root intermediate certs

echo "✅ Finished. Retained files:"
ls -1 root-cert.pem ca-cert.pem ca-key.pem ca-crl.pem cert-chain.pem

echo "✅ All done."
