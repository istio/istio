#!/bin/bash

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

set -e

# Expects root directory to be the base of the Istio repo.

export TESTDATA=$(cd ./tests/testdata && pwd)

# Where to put the generated certs:
# rootca - the roots
# clusters/NAME - one for each cluster
# clusters/NAME/etc/cacerts - the Istio CA for the cluster
export OUT=${OUT:-${TESTDATA}/certs_tls}

# This is a new version of the old script in security/samples/certs, using the new naming.
# It can generate root CA, cluster CA, Istio CA and workload certificates using only openssl
#
# It is intended as an example and for testing - you should customize the
# certificate configs to fit your needs.
#
# When running Istiod on a VM, run this script from the working directory where Istiod
# is started.,

# Generate key and cert for root CA.
# The root key will normally be kept offline, not saved to the cluster
# This function generates 2 roots, to validate multiple roots behavior
# The 'default' root used in the rest of the script is the EC256 one, since it has
# less coverage and exposure.
#
# Normally ca.crt should include old roots when rotating the keys.
#

function rootCA() {
  local meshname=${1:-mesh.internal}

  mkdir -p ${OUT}/rootca
  cd ${OUT}/rootca

  # Save the meshname - will be used as Org name in the certs.
  echo $meshname > meshname

  cat > ca.cfg <<EOF
[req]
req_extensions = v3_req
prompt = no

[v3_req]
keyUsage = critical, digitalSignature, keyEncipherment, keyCertSign, cRLSign
extendedKeyUsage = serverAuth, clientAuth
basicConstraints = critical, CA:true
EOF


  # this generates both EC Parameters and EC Private key blocks. Istio doesn't like that.
  openssl ecparam -genkey -name prime256v1  -out ec_tls.pem
  openssl ec -in ec_tls.pem  -out tls.key

  openssl req -new -sha256 -key tls.key -out tls.csr -config ca.cfg \
     -subj "/C=US/O=${meshname}/CN=${meshname} root1"

  openssl req -x509 -sha256 -days 365 -key tls.key -in tls.csr -out leaf_tls.crt

  cp leaf_tls.crt ec_ca.crt

  # Generate a second RSA root
  openssl req -newkey rsa:2048 -nodes -keyout rsa-tls.key -x509 -out rsa_ca.crt \
    -days 36500 --config ca.cfg \
    -subj "/C=US/O=${meshname}/CN=${meshname} root2"

  # New name, 2 roots.
  cat ec_ca.crt rsa_ca.crt > ca.crt

  # This only shows first cert
  #openssl x509 -in ca.crt -text -noout
  openssl crl2pkcs7 -nocrl -certfile ca.crt | openssl pkcs7 -print_certs \
    -noout -text | egrep "Subject:|Issuer:"

  # TODO: generate files suitable for /etc/ssl/certs
  # TODO: regenerate /etc/ssl/certs/ca-certificates.crt to include the mesh roots

  openssl verify -verbose -CAfile ca.crt leaf_tls.crt
}

# Generate a root cert for a cluster, signed by the root
# This key should also be kept offline or made available for signing certs for
# the cluster without exposing the top level root.
function clusterCA() {
  local cluster=${1:-cluster1}

  mkdir -p ${OUT}/clusters/$cluster
  cd ${OUT}/clusters/$cluster

  # Copy root CA, the template and meshname
  local parentDir=${ROOT_CA:-${OUT}/rootca}
  cp ${parentDir}/ca.crt .
  cp ${parentDir}/meshname .
  cp ${parentDir}/ca.cfg .

  local meshname=$(cat meshname)

  openssl ecparam -genkey -name prime256v1  -out ec_tls.pem
  openssl ec -in ec_tls.pem  -out tls.key

  openssl req -new -sha256 -key tls.key -out tls.csr -config ca.cfg \
     -subj "/C=US/O=${meshname}/OU=${cluster}/CN=${meshname} root1"

  openssl x509 -req -days 36500 -sha256 \
      -in tls.csr \
      -CA ca.crt \
      -CAkey ../rootca/tls.key \
      -out leaf_tls.crt \
      -CAcreateserial  -extensions v3_req \
      -extfile ca.cfg

  # This is a cluster cert, signed by the root - no chain needed.
  cp leaf_tls.crt tls.crt

  openssl crl2pkcs7 -nocrl -certfile tls.crt | openssl pkcs7 -print_certs -noout \
   -text | egrep "Subject:|Issuer:"
  openssl verify -verbose -CAfile ca.crt leaf_tls.crt
}

# Generate private key for Istio CA.
function istioCA() {
  local cluster=${1:-cluster1}

  WD=${OUT}/clusters/${cluster}/etc/cacerts
  mkdir -p ${WD}
  cd ${WD}

  local trustDomain=${TRUST_DOMAIN:-cluster.local}

  local parentDir=${CLUSTER_CA:-${OUT}/clusters/${cluster}}
  cp ${parentDir}/ca.crt .
  cp ${parentDir}/meshname .
  cp ${parentDir}/ca.cfg .
  cp ${parentDir}/tls.crt parent_tls.crt
  echo ${trustDomain} > trustdomain

  local meshname=$(cat meshname)

  echo "Issuing Istio CA for"
  echo "cluster=${cluster}"
  echo "trustDomain=${trustDomain}"
  echo "mesh=${meshname}"

  openssl ecparam -genkey -name prime256v1  -out ec_tls.pem
  openssl ec -in ec_tls.pem  -out tls.key

  openssl req -new -sha256 -key tls.key -out tls.csr -config ca.cfg \
     -subj "/C=US/O=${meshname}/OU=${cluster}/OU=${trustDomain}/CN=${meshname} istio root"

  openssl x509 -req -days 36500 -sha256 \
      -in tls.csr \
      -CA ${parentDir}/tls.crt \
      -CAkey ${parentDir}/tls.key \
      -out leaf_tls.crt \
      -CAcreateserial  -extensions v3_req \
      -extfile ca.cfg

  # This is a cluster cert, signed by the root - no chain needed.
  cat leaf_tls.crt parent_tls.crt > tls.crt

  openssl crl2pkcs7 -nocrl -certfile tls.crt | openssl pkcs7 -print_certs -noout -text | egrep "Subject:|Issuer:"


}


# Generate private key for Istio CA.
function workload() {
  local cluster=${1:-cluster1}
  local ns=${2:-default}
  local ksa=${3:-default}

  local WD=${OUT}/clusters/${cluster}/${ns}_${ksa}/var/run/secrets/workload-spiffe-credentials
  mkdir -p ${WD}
  cd ${WD}

  local parentDir=${ISTIO_CA:-${OUT}/clusters/${cluster}/etc/cacerts}

  cp ${parentDir}/ca.crt .
  cp ${parentDir}/tls.crt parent_tls.crt
  cp ${parentDir}/meshname .
  cp ${parentDir}/trustdomain .

  local trustDomain=$(cat trustdomain)

  san="spiffe://${trustDomain:-cluster.local}/ns/$ns/sa/$ksa"

  cat > workload.cfg <<EOF
[req]
req_extensions = v3_req
x509_extensions = v3_req
prompt = no

[v3_req]
keyUsage = critical, digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
basicConstraints = critical, CA:FALSE
subjectAltName = critical, @alt_names

[alt_names]
URI = $san
EOF

  local meshname=$(cat meshname)

  openssl ecparam -genkey -name prime256v1  -out ec_tls.pem
  openssl ec -in ec_tls.pem  -out tls.key

  # Using K8S conventions:
  # Organization: ["system:nodes"], common name starts with "system:node:".
  # https://kubernetes.io/docs/reference/access-authn-authz/authentication
  # CN=user name, O=group (repeated)
  openssl req -new -sha256 -key tls.key -out tls.csr -config workload.cfg \
     -subj "/C=US/O=${meshname}/OU=${cluster}/CN=${ns} ${ksa}"

  openssl x509 -req -days 36500 -sha256 \
      -in tls.csr \
      -CA ${parentDir}/tls.crt \
      -CAkey ${parentDir}/tls.key \
      -out leaf_tls.crt \
      -CAcreateserial  -extensions v3_req \
      -extfile workload.cfg

  # This is a cluster cert, signed by the root - no chain needed.
  cat leaf_tls.crt ${parentDir}/tls.crt > tls.crt

  echo "Generated certificate chain"
  openssl crl2pkcs7 -nocrl -certfile tls.crt | openssl pkcs7 -print_certs -noout -text | egrep "Subject:|Issuer:"

  echo "Roots"
  openssl crl2pkcs7 -nocrl -certfile ca.crt | openssl pkcs7 -print_certs -noout -text | egrep "Subject:|Issuer:"


  openssl verify -verbose -CAfile ca.crt -untrusted parent_tls.crt -show_chain leaf_tls.crt

}

function localEnv() {
  local istiodDir=${1:-/tmp}

  mkdir -p ${istiodDir}/etc/cacerts
  cp cluster1/istioca/ca.crt cluster1/istioca/tls.crt cluster1/istioca/tls.key ${istiodDir}/etc/cacerts
}

function all() {
  (rootCA)
  (clusterCA)
  (istioCA)
  (workload)
  (localEnv)
}

# Quick hack to run one of the functions above
CMD=$1
shift
$CMD $*
