#!/bin/bash

# Copyright Istio Authors
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

# Generates the a CA cert, a server key/cert, client key/cert signed by
# the CA.
#
# reference: https://github.com/kubernetes/kubernetes/blob/master/plugin/pkg/admission/webhook/gencerts.sh

set -e

cat > client.conf <<EOF
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
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

cat > server.conf <<EOF
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
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

outfile=testcerts.go

# Create a certificate authority
openssl genrsa -out CAKey.pem 2048
openssl req -x509 -new -nodes -key CAKey.pem -days 100000 -out CACert.pem -subj "/CN=${CN_BASE}_ca"

# Create a server certificate
openssl genrsa -out ServerKey.pem 2048
openssl req -new -key ServerKey.pem -out server.csr -subj "/CN=${CN_BASE}_server" -config server.conf
openssl x509 -req -in server.csr -CA CACert.pem -CAkey CAKey.pem -CAcreateserial -out ServerCert.pem -days 100000 -extensions v3_req -extfile server.conf

# Create a client certificate
openssl genrsa -out ClientKey.pem 2048
openssl req -new -key ClientKey.pem -out client.csr -subj "/CN=${CN_BASE}_client" -config client.conf
openssl x509 -req -in client.csr -CA CACert.pem -CAkey CAKey.pem -CAcreateserial -out ClientCert.pem -days 100000 -extensions v3_req -extfile client.conf

cat > $outfile << EOF
/*
Copyright Istio Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

EOF

{
	echo "// This file was generated using openssl by the generate-certs.sh script"
	echo "// and holds raw certificates for the webhook tests."
	echo ""
	echo "package testcerts"
} >> $outfile

for file in CACert ServerKey ServerCert ClientKey ClientCert; do
	data=$(cat ${file}.pem)
	{
		echo ""
		echo "// ${file} is a test cert for dynamic admission controller."
		echo "var $file = []byte(\`$data\`)"
	} >> $outfile
done

# Clean up after we're done.
rm ./*.pem
rm ./*.csr
rm ./*.srl
rm ./*.conf
