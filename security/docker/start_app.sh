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

echo "Starting NodeAgent..."
# Run node-agent
/usr/local/bin/node_agent \
  --env onprem \
  --cert-chain /usr/local/bin/node_agent.crt \
  --key /usr/local/bin/node_agent.key \
  --workload-cert-ttl 90s \
  --root-cert /usr/local/bin/istio_ca.crt >/var/log/node-agent.log 2>&1 &

echo "Starting Application..."
# Start app
apt-get update && apt-get -y install curl
curl -sL https://deb.nodesource.com/setup_8.x | bash -
apt-get install -y nodejs
npm install express
node /usr/local/bin/app.js
