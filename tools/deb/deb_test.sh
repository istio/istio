#!/bin/bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
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
#
################################################################################
#
# Test for istio debian. Will run in a docker image where the .deb has been installed.

function startIstio() {
    /usr/local/bin/hyperistio --envoy=false &
    sleep 1

    bash -x /usr/local/bin/istio-start.sh &
    sleep 1
}

function istioDebug() {
    curl localhost:15000/logging?upstream=debug
    curl localhost:15000/logging?client=debug
    curl localhost:15000/logging?connection=debug
    curl localhost:15000/logging?http2=debug
    curl localhost:15000/logging?grpc=debug
}

function istioStats() {
    curl localhost:15000/stats

    # Try to get the endpoints over https
    curl -k --key tests/testdata/certs/default/key.pem \
        --cert tests/testdata/certs/default/cert-chain.pem  \
        -v https://istio-pilot.istio-system:15011/debug/endpointz
}

function istioTest() {
    # Will go to local machine
    su -s /bin/bash -c "curl -v byon-docker.test.istio.io:7072" istio-test
}
