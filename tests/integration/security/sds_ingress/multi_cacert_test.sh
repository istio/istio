#! /bin/bash
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

# Generates test CAs and matching test client certs and confirm you can add/remove CAs without error to
# traffic to existing ones
#
# Related to
# https://istio.io/latest/docs/tasks/traffic-management/ingress/secure-ingress/#configure-a-mutual-tls-ingress-gateway

# This assumes/has been tested on AWS EKS cluster with fortio 1.17 on client and server (as echo debug server)

set -ex

# In Envoy custom log format: %DOWNSTREAM_PEER_SUBJECT% should show the CN of the client cert
# Certs are also forwarded in X-Forwarded-Client-Cert http headers

HOST=testca1.istio.io

function gen_root_ca {
    SUFFIX=$1
    openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:2048 \
        -subj "/O=TestCA${SUFFIX}_o/CN=TestCA${SUFFIX}_root" -keyout "ca${SUFFIX}_r.key" -out "ca${SUFFIX}_r.crt"
}

function gen_intermediate_ca {
    SUFFIX=$1
    openssl req -sha256 -nodes -days 365 -newkey rsa:2048 \
        -subj "/O=TestCA${SUFFIX}_o/CN=TestCA${SUFFIX}_intermediate" -keyout "ca${SUFFIX}_i.key" -out "ca${SUFFIX}_i.csr"
    openssl x509 -req -days 180 -CA "ca${SUFFIX}_r.crt" -CAkey "ca${SUFFIX}_r.key" -set_serial 1 \
        -in "ca${SUFFIX}_i.csr" -out "ca${SUFFIX}_i.crt"
}

# Client signed by intermediate CAs
function gen_cli {
    SUFFIX=$1
    openssl req -out "cli$SUFFIX.csr" -newkey rsa:2048 -nodes -keyout "cli$SUFFIX.key" \
        -subj "/CN=TEST_CLI${SUFFIX}_001/O=Client test org${SUFFIX}"
    openssl x509 -req -days 30 -CA "ca${SUFFIX}_i.crt" -CAkey "ca${SUFFIX}_i.key" -set_serial 1 \
        -in "cli$SUFFIX.csr" -out "cli$SUFFIX.crt"
}

# different CA for server - single cert
function gen_server {
    SUFFIX=SRV
    gen_root_ca $SUFFIX
    openssl req -out srv.csr -newkey rsa:2048 -nodes -keyout srv.key \
        -subj "/CN=$HOST/O=Server test organization" \
        -reqexts SAN \
        -config <(cat /etc/ssl/openssl.cnf \
            <(printf "\n[SAN]\nsubjectAltName=DNS:%s" "$HOST"))
    openssl x509 -req -days 90 -CA caSRV_r.crt -CAkey caSRV_r.key -set_serial 0 \
        -in srv.csr -out srv.crt \
        -extfile <(printf "subjectAltName=DNS:%s\n" "$HOST")
}


function add_ingress {
    SUFFIX=$1
    cat <<_EOF_ | sed -e "s/SUFFIX/$SUFFIX/g" -e "s/HOST/$HOST/g" | tee >(cat 1>&2) | kubectl apply -f -
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata:
 name: mtls-test-gatewaySUFFIX
 namespace: istio-system
spec:
 selector:
   istio: ingressgateway
 servers:
 - port:
     number: 443
     name: https
     protocol: HTTPS
   tls:
     mode: MUTUAL
     credentialName: testSUFFIX-credential # must be the same as secret
   hosts:
   - HOST
---
apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: fortio-ca-debug
  namespace: istio-system
spec:
  hosts:
  - HOST
  gateways:
  - mtls-test-gatewaySUFFIX
  http:
  - match:
    - uri:
        prefix: /fortio
    route:
      - destination:
          host: fortio-client.fortio.svc.cluster.local
          port:
            number: 8080
_EOF_
}

# Experimentally it takes less than 10s for SDS changes to propagate to envoy at the ingress,
# from changing the configmap/secret
function wait_for_propagation {
  sleep 10
}

gen_server

for suffix in 1 2 ; do
    gen_root_ca "CLI$suffix"
    gen_intermediate_ca "CLI$suffix"
    gen_cli "CLI$suffix"
    # Make a bundle/chain for each CA (intermediate+root)
    cat caCLI${suffix}_*.crt > "caCLI$suffix.crt"
    kubectl delete -n istio-system secret "test$suffix-credential" || true
    kubectl create -n istio-system secret tls "test$suffix-credential" \
        --key=srv.key --cert=srv.crt
    # Seperate the CA config map from the server cert one using credential-cacert:
    kubectl delete -n istio-system secret "test$suffix-credential-cacert" || true
    kubectl create -n istio-system secret generic "test$suffix-credential-cacert" \
        --from-file="ca.crt=caCLI$suffix.crt"
done

# Both/All CAs in a bundle:
cat caCLI?.crt caCLI?_i.crt > caCLIall.crt

ls -l -- *.crt

add_ingress 1

function bothCA {
  kubectl create -n istio-system secret generic test1-credential-cacert \
    --from-file=ca.crt=caCLIall.crt --dry-run=client -o yaml | kubectl apply -f -
}

function only1CA {
   kubectl create -n istio-system secret generic test1-credential-cacert \
    --from-file=ca.crt=caCLI1.crt --dry-run=client -o yaml | kubectl apply -f -
}

wait_for_propagation

# This is the "hostname" style of LBs (e.g AWS)
INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')
INGRESS_IP=$(nslookup "$INGRESS_HOST" | awk '/^Address:/ {print $2}'|tail -1)

echo "Ingress served by $INGRESS_HOST ($INGRESS_IP)"

function singleCall {
  SUFFIX=$1
#  fortio curl is easier with resolve but regular curl is more standard.
#  fortio curl -resolve $INGRESS_HOST -cert cliCLI$SUFFIX.crt -key cliCLI$SUFFIX.key -cacert caSRV.crt  https://$HOST/fortio/debug/
  curl -v --resolve "$HOST:443:$INGRESS_IP" --cert "cliCLI$SUFFIX.crt" --key "cliCLI$SUFFIX.key" --cacert caSRV_r.crt  "https://$HOST/fortio/debug/"
}

function check2fail {
  set +e
  if singleCall 2
  then
    echo "** call using cli cert/ca 2 should have failed"
    exit 1
  else
    echo "cli2 failed as expected"
  fi
  set -e
}

# We start with only 1 CA:
wait_for_propagation

# Should succeed
singleCall 1
# Also start fortio test during the changes of CA, on cli1 should not get any errors:
RES_FILE=fortio_ca_test.json
fortio load -json $RES_FILE -jitter -c 2 -qps 10 -t 0 -resolve "$INGRESS_HOST" -cert cliCLI1.crt -key cliCLI1.key -cacert caSRV_r.crt  "https://$HOST/fortio/debug/" &
FORTIO_PID=$!

# Should fail
check2fail

# We switch to 2 CAs:
bothCA ; wait_for_propagation

# 1 should still work
singleCall 1
# but 2 should now work too
singleCall 2

# Back to only 1 CA:

only1CA ; wait_for_propagation
# 1 should still work
singleCall 1
# 2 should fail again
check2fail

kill -int $FORTIO_PID
wait

TOTAL_REQUESTS=$(jq .DurationHistogram.Count < $RES_FILE)
OK_REQUESTS=$(jq .RetCodes.\"200\" < $RES_FILE)

if [[ "$TOTAL_REQUESTS" != "$OK_REQUESTS" ]]
then
  echo "Errors found $TOTAL_REQUESTS != $OK_REQUESTS"
  jq .RetCodes < $RES_FILE
fi

echo "*** All tested passed"
