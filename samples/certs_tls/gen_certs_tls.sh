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

# Expects root directory to be the base of the Istio repo.
BASE=$(cd $(dirname $0)/../.. && pwd)

TESTDIR=$(cd ${BASE}/tests/testdata/certs_tls && pwd)

#------------------------------------------------------------------------
# variables: workload certs: eg VM
WORKLOAD_DAYS ?= 1
SERVICE_ACCOUNT ?= default
WORKLOAD_CN ?= Workload


# Output base directory for the script. For leaf certs, files are created directly in the directory.
# For CA, istiod or agent env - the expected directory structure is created.
#
# rootca - the roots
# clusters/NAME - one for each cluster
# clusters/NAME/etc/cacerts - the Istio CA for the cluster
export OUT=${OUT:-$TESTDIR}

# CA is the base directory for the parent CA for each step (except generating the root)
export CA=${CA:-${TESTDIR}}

# rootCA generates a pair of root certs, one RSA and one ECDSA.
# The ca.crt file should include both roots.
# TODO: use different expiration, add option to test short root expiry
function rootCA() {
  local meshname=${1:-mesh.internal}

  mkdir -p ${OUT}/rootca

  # Save the meshname - will be used as Org name in the certs.
  echo $meshname > ${OUT}/rootca/meshname

  cat > ${OUT}/rootca/ca.cfg <<EOF
[req]
req_extensions = v3_req
prompt = no

[v3_req]
keyUsage = critical, digitalSignature, nonRepudiation, keyEncipherment, keyCertSign, cRLSign
extendedKeyUsage = serverAuth, clientAuth
basicConstraints = critical, CA:true
EOF
  # basicConstraints = critical, CA:true, pathlen:0" - used in the old makefile ?


  # this generates both EC Parameters and EC Private key blocks. Istio doesn't like that.
  openssl ecparam -genkey -name prime256v1  -out ${OUT}/rootca/ec_tls.pem
  openssl ec -in ${OUT}/rootca/ec_tls.pem  -out ${OUT}/rootca/tls.key

  openssl req -new -sha256 -key ${OUT}/rootca/tls.key -out ${OUT}/rootca/tls.csr -config ${OUT}/rootca/ca.cfg \
     -subj "/C=US/O=${meshname}/CN=${meshname} root1"

  openssl req -x509 -sha256 -days 365 -key ${OUT}/rootca/tls.key -in ${OUT}/rootca/tls.csr -out ${OUT}/rootca/leaf_tls.crt

  cp ${OUT}/rootca/leaf_tls.crt ${OUT}/rootca/ec_ca.crt

  # Generate a second RSA root
  openssl req -newkey rsa:2048 -nodes -keyout ${OUT}/rootca/rsa-tls.key -x509 -out ${OUT}/rootca/rsa_ca.crt \
    -days 36500 --config ${OUT}/rootca/ca.cfg \
    -subj "/C=US/O=${meshname}/CN=${meshname} root2"

  # New name, 2 roots.
  cat ${OUT}/rootca/ec_ca.crt ${OUT}/rootca/rsa_ca.crt > ${OUT}/rootca/ca.crt

  # This only shows first cert
  #openssl x509 -in ca.crt -text -noout
  openssl crl2pkcs7 -nocrl -certfile ${OUT}/rootca/ca.crt | openssl pkcs7 -print_certs \
    -noout -text | egrep "Subject:|Issuer:"

  # TODO: generate files suitable for /etc/ssl/certs
  # TODO: regenerate /etc/ssl/certs/ca-certificates.crt to include the mesh roots

  openssl verify -verbose -CAfile ${OUT}/rootca/ca.crt ${OUT}/rootca/leaf_tls.crt
}

# Generate a root cert for a cluster ($1), signed by the root
#
# This key should also be kept offline or made available for signing certs for
# the cluster without exposing the top level root.
# Mainly used to test deeper chains.
function clusterCA() {
  local cluster=${1:-cluster1}

  local o=${OUT}/clusters/$cluster/cluster-ca
  mkdir -p ${o}

  # Copy root CA, the template and meshname
  local parentDir=${CA}/rootca
  cp ${parentDir}/ca.crt ${o}
  cp ${parentDir}/meshname ${o}
  cp ${parentDir}/ca.cfg ${o}

  local meshname=$(cat ${o}/meshname)

  openssl ecparam -genkey -name prime256v1  -out ${o}/ec_tls.pem
  openssl ec -in ${o}/ec_tls.pem  -out ${o}/tls.key

  openssl req -new -sha256 -key ${o}/tls.key -out ${o}/tls.csr -config ${parentDir}/ca.cfg \
     -subj "/C=US/O=${meshname}/OU=${cluster}/CN=${meshname} root1"

  openssl x509 -req -days 36500 -sha256 \
      -in ${o}/tls.csr \
      -CA ${o}/ca.crt \
      -CAkey ${parentDir}/tls.key \
      -out ${o}/leaf_tls.crt \
      -CAcreateserial  -extensions v3_req \
      -extfile ${parentDir}/ca.cfg

  # This is a cluster cert, signed by the root - no chain needed.
  cp ${o}/leaf_tls.crt ${o}/tls.crt

  openssl crl2pkcs7 -nocrl -certfile ${o}/tls.crt | openssl pkcs7 -print_certs -noout \
   -text | egrep "Subject:|Issuer:"

  # No intermediate - directly signed by root.
  openssl verify -verbose -CAfile ${o}/ca.crt ${o}/leaf_tls.crt
}

# Generate private key for Istio CA, using the (offline) cluster root (named $1).
istioCA() {
  local cluster=${1:-cluster1}
  local trustDomain=${TRUST_DOMAIN:-cluster.local}

  local o=${OUT}/clusters/${cluster}/istiod/etc/cacerts
  mkdir -p ${o}
  # Copy root CA, the template and meshname
  local parentDir=${CA}/clusters/${cluster}/cluster-ca

  cp ${parentDir}/ca.crt ${o}
  cp ${parentDir}/meshname ${o}
  cp ${parentDir}/ca.cfg ${o}
  cp ${parentDir}/tls.crt ${o}/parent_tls.crt
  echo ${trustDomain} > ${o}/trustdomain

  local meshname=$(cat ${parentDir}/meshname)

  echo "Issuing Istio CA for"
  echo "cluster=${cluster}"
  echo "trustDomain=${trustDomain}"
  echo "mesh=${meshname}"

  openssl ecparam -genkey -name prime256v1  -out ${o}/ec_tls.pem
  openssl ec -in ${o}/ec_tls.pem  -out ${o}/tls.key

  openssl req -new -sha256 -key ${o}/tls.key -out ${o}/tls.csr -config ${o}/ca.cfg \
     -subj "/C=US/O=${meshname}/OU=${cluster}/OU=${trustDomain}/CN=${meshname} istio root"

  openssl x509 -req -days 36500 -sha256 \
      -in ${o}/tls.csr \
      -CA ${parentDir}/tls.crt \
      -CAkey ${parentDir}/tls.key \
      -out ${o}/leaf_tls.crt \
      -CAcreateserial  -extensions v3_req \
      -extfile ${o}/ca.cfg

  cat ${o}/leaf_tls.crt ${o}/parent_tls.crt > ${o}/tls.crt

  openssl crl2pkcs7 -nocrl -certfile ${o}/tls.crt | openssl pkcs7 -print_certs -noout -text | egrep "Subject:|Issuer:"

}


# Generate private key for Istiod.
function istiod() {
  local cluster=${1:-cluster1}
  local ns=${2:-default}
  local ksa=${3:-default}

  # Signed with the offline root CA
  local parentDir=${CA}/clusters/${cluster}/istiod/etc/cacerts
  local o=${OUT}/clusters/${cluster}/istiod/var/run/secrets/istiod/tls

  _workload_init ${o} ${parentDir}

  echo "DNS = istiod.istio-system.svc" >> ${o}/workload.cfg
  echo "DNS = istiod.istio-system.svc.${cluster}.${meshname}" >> ${o}/workload.cfg

  _workload_sign ${o} ${parentDir} "/C=US/O=${meshname}/OU=${cluster}/CN=${ns} ${ksa}"

}

# Generate workload certificates
function workload() {
    local cluster=${1:-cluster1}
    local ns=${2:-default}
    local ksa=${3:-default}

    # Signed with the offline root CA
    local parentDir=${CA}/clusters/${cluster}/istiod/etc/cacerts
    local o=${OUT}/clusters/${cluster}/istiod/var/run/secrets/istiod/tls

    _workload_init ${o} ${parentDir}

    echo "DNS = istiod.istio-system.svc" >> ${o}/workload.cfg
    echo "DNS = istiod.istio-system.svc.${cluster}.${meshname}" >> ${o}/workload.cfg

    _workload_sign ${o} ${parentDir} "/C=US/O=${meshname}/OU=${cluster}/CN=${ns} ${ksa}"
}



_workload_init() {
  local o=${1}
  local parentDir=${2}

  # Can also sign with cacerts - the Istiod code doesn't do that.
  # local parentDir=${CA}/clusters/${cluster}/istiod/etc/cacerts

  mkdir -p ${o}

  cp ${parentDir}/ca.crt ${o}
  cp ${parentDir}/tls.crt ${o}/parent_tls.crt
  cp ${parentDir}/meshname ${o}
  cp ${parentDir}/trustdomain ${o}

  local trustDomain=$(cat ${o}/trustdomain)
  local meshname=$(cat ${o}/meshname)

  san="spiffe://${trustDomain}/ns/istio-system/sa/istiod"

  cat > ${o}/workload.cfg <<EOF
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

}

# _workload_sign will sign a cert using workload.cfg and the parent key.
_workload_sign() {
  local o=${1}
  local parentDir=${2}
  local subj=${3}

  openssl ecparam -genkey -name prime256v1  -out ${o}/ec_tls.pem
  openssl ec -in ${o}/ec_tls.pem  -out ${o}/tls.key

  openssl req -new -sha256 -key ${o}/tls.key -out ${o}/tls.csr -config ${o}/workload.cfg \
     -subj "${subj}"

  openssl x509 -req -days 36500 -sha256 \
      -in ${o}/tls.csr \
      -CA ${parentDir}/tls.crt \
      -CAkey ${parentDir}/tls.key \
      -out ${o}/leaf_tls.crt \
      -CAcreateserial  -extensions v3_req \
      -extfile ${o}/workload.cfg

  cat ${o}/leaf_tls.crt ${parentDir}/tls.crt > ${o}/tls.crt

  echo "Generated certificate chain"
  openssl crl2pkcs7 -nocrl -certfile ${o}/tls.crt | openssl pkcs7 -print_certs -noout -text | egrep "Subject:|Issuer:"

  echo "Roots"
  openssl crl2pkcs7 -nocrl -certfile ${o}/ca.crt | openssl pkcs7 -print_certs -noout -text | egrep "Subject:|Issuer:"


  openssl verify -verbose -CAfile ${o}/ca.crt -untrusted ${o}/parent_tls.crt -show_chain ${o}/leaf_tls.crt
}



function all() {
  (rootCA)
  (clusterCA)
  (istioCA)
  (istiod)
  (workload)
}

# Quick hack to run one of the functions above
CMD=$1
shift
$CMD $*
