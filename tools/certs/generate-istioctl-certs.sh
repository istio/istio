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

# Generates files for istioctl's --cert-dir directory.

set -o errexit

if [[ "x$DEBUG_SHELL_SCRIPT" != "x" ]]; then
   set -x
fi

if [ -z "${ISTIO_NAMESPACE}" ]; then
   export ISTIO_NAMESPACE=istio-system
   echo '$ISTIO_NAMESPACE not set, defaulting to' $ISTIO_NAMESPACE
fi

if [ -z "${CERT_DIR}" ]; then
   export CERT_DIR=istioctl-certs
   echo '$CERT_DIR not set, defaulting to' $CERT_DIR
fi

echo ""

mkdir -p ${CERT_DIR}
kubectl get secret istio-ca-secret -n ${ISTIO_NAMESPACE} -o "jsonpath={.data['ca-cert\.pem']}" | base64 -d > ${CERT_DIR}/k8s-root-cert.pem
kubectl get secret istio-ca-secret -n ${ISTIO_NAMESPACE} -o "jsonpath={.data['ca-key\.pem']}" | base64 -d > ${CERT_DIR}/k8s-root-key.pem
openssl genrsa -out ${CERT_DIR}/ca-key.pem 4096
set +o errexit
cat > ${CERT_DIR}/workload.conf << EOF
[ req ]
encrypt_key = no
prompt = no
utf8 = yes
default_md = sha256
default_bits = 4096
req_extensions = req_ext
x509_extensions = req_ext
distinguished_name = req_dn
[ req_ext ]
subjectKeyIdentifier = hash
basicConstraints = critical, CA:false
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName=@san
[ san ]
URI.1 = spiffe://cluster.local/ns/istioctl-itp/sa/default
DNS.1 = spiffe://cluster.local/ns/istioctl-itp/sa/default
[ req_dn ]
O = Istio
CN = Workload
L = istioctl-itp
EOF
set -o errexit
openssl genrsa -out ${CERT_DIR}/key.pem 4096
openssl req -new -config ${CERT_DIR}/workload.conf -key ${CERT_DIR}/key.pem -out ${CERT_DIR}/workload.csr
openssl x509 -req -days 1 \
  -CA ${CERT_DIR}/k8s-root-cert.pem  -CAkey ${CERT_DIR}/k8s-root-key.pem -CAcreateserial\
  -extensions req_ext -extfile ${CERT_DIR}/workload.conf \
  -in ${CERT_DIR}/workload.csr -out ${CERT_DIR}/workload-cert.pem
# Now give the files the name istioctl expects
mv ${CERT_DIR}/workload-cert.pem ${CERT_DIR}/cert-chain.pem
mv ${CERT_DIR}/k8s-root-cert.pem ${CERT_DIR}/root-cert.pem
# Now remove the files that istioctl doesn't use
rm ${CERT_DIR}/ca-key.pem ${CERT_DIR}/k8s-root-key.pem ${CERT_DIR}/*.srl ${CERT_DIR}/*.conf ${CERT_DIR}/*.csr

echo ""
echo ""
echo ""
echo istioctl Certificates created!
echo Use "'--cert-dir ${CERT_DIR}'"
echo For example,
echo "  "  istioctl x version --xds-address localhost:15012 --authority istiod.istio-system.svc --cert-dir ${CERT_DIR}
