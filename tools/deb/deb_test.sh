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

export ISTIO_SERVICE_CIDR=10.1.1.0/24

# Echo server
export ISTIO_INBOUND_PORTS=7070,7072,7073,7074,7075

# Try out with TPROXY - the auth variant uses redirect
#export ISTIO_INBOUND_INTERCEPTION_MODE=TPROXY

#export ISTIO_PILOT_PORT=15005
#export ISTIO_CP_AUTH=MUTUAL_TLS


/usr/local/bin/hyperistio --envoy=false &
sleep 1

bash -x /usr/local/bin/istio-start.sh &
sleep 1

curl localhost:15000/stats
# Will go to local machine
su -s /bin/bash -c "curl -v byon-docker.test.istio.io:7072" istio-test
curl localhost:15000/stats

