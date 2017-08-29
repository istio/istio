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
# Run a local pilot, local envoy, [local mixer], using a hand-crafted config.

# Assumes proxy is in a src/proxy directory (as created by repo). Override WS env to use a different base dir
# or make sure GOPATH is set to the base of the 'go' workspace
WS=${WS:-$(pwd)/../..}
export GOPATH=${GOPATH:-$WS/go}
LOG_DIR=${LOG_DIR:-log/integration}
PILOT=${PILOT:-${GOPATH}/src/istio.io/pilot}

# Build debian and binaries for all components we'll test on the VM
# Will checkout mixer, pilot and proxy in the expected locations/
function build_all() {
  mkdir -p $WS/go/src/istio.io


  if [[ -d $GOPATH/src/istio.io/pilot ]]; then
    (cd $GOPATH/src/istio.io/pilot; git pull upstream master)
  else
    (cd $GOPATH/src/istio.io; git clone https://github.com/istio/pilot)
  fi

  if [[ -d $GOPATH/src/istio.io/istio ]]; then
    (cd $GOPATH/src/istio.io/istio; git pull upstream master)
  else
    (cd $GOPATH/src/istio.io; git clone https://github.com/istio/istio)
  fi

  if [[ -d $GOPATH/src/istio.io/mixer ]]; then
    (cd $GOPATH/src/istio.io/mixer; git pull upstream master)
  else
    (cd $GOPATH/src/istio.io; git clone https://github.com/istio/mixer)
  fi

  pushd $GOPATH/src/istio.io/pilot
  bazel build ...
  ./bin/init.sh
  popd

  (cd $GOPATH/src/istio.io/mixer; bazel build ...)
  bazel build tools/deb/...

}

function kill_all() {
  if [[ -f $LOG_DIR/pilot.pid ]] ; then
    kill -9 $(cat $LOG_DIR/pilot.pid)
    kill -9 $(cat $LOG_DIR/mixer.pid)
    kill -9 $(cat $LOG_DIR/envoy.pid)
    kill -9 $(cat $LOG_DIR/test_server.pid)
  fi
}

# Start pilot, envoy and mixer for local integration testing.
function start_all() {
  mkdir -p $LOG_DIR
  POD_NAME=pilot POD_NAMESPACE=default  ${PILOT}/bazel-bin/cmd/pilot-discovery/pilot-discovery discovery -n default --kubeconfig ~/.kube/config &
  echo $! > $LOG_DIR/pilot.pid

  ${GOPATH}/src/istio.io/mixer/bazel-bin/cmd/server/mixs server --configStoreURL=fs:${GOPATH}/src/istio.io/mixer/testdata/configroot -v=2 --logtostderr &
  echo $! > $LOG_DIR/mixer.pid

  ${GOPATH}/src/istio.io/pilot/bazel-bin/test/server/server --port 9999 > $LOG_DIR/test_server.log 2>&1 &
  echo $! > $LOG_DIR/test_server.pid

  # 'lds' disabled, so we can use manual config.
  bazel-bin/src/envoy/mixer/envoy -c tools/deb/test/envoy_local.json --restart-epoch 0 --drain-time-s 2 --parent-shutdown-time-s 3 --service-cluster istio-proxy --service-node sidecar~172.17.0.2~mysvc.~svc.cluster.local &
  echo $! > $LOG_DIR/envoy.pid
}

# Add a service and endpoint to K8S.
function istioAddEndpoint() {
  NAME=${1:-}
  PORT=${2:-}
  ENDPOINT_IP=${3:-$(ip route get 8.8.8.8 | head -1 | cut -d' ' -f8)}

  cat << EOF | kubectl apply -f -
kind: Service
apiVersion: v1
metadata:
  name: $NAME
spec:
  ports:
    - protocol: TCP
      port: $PORT
      name: http

---

kind: Endpoints
apiVersion: v1
metadata:
  name: $NAME
subsets:
  - addresses:
      - ip: $ENDPOINT_IP
    ports:
      - port: $PORT
        name: http
EOF
}

# Standalone test verifies:
# - pilot standalone (running in a VM, outside k8s) works
# - services registered by the VM are reachable ( RDS + SDS )
# - outgoing calls with http_proxy work.
function test_standalone() {

  # Register the service running on the local test machine. Uses the primary IP of the host running the test.
  istioAddEndpoint test-proxy 9999

  # Pending the 'merged namespaces in rds for proxy', we have the proxy use the existing config for port 9999

  # Verify we can connect to the endpoint - using explicitly/manual configured cluster and route
  # This confirms proxy mode works in the envoy build.
  curl -x http://127.0.0.1:15003 http://test-proxy.default.svc.cluster.local:9999

  # Verify we can connect using RDS and SDS. This confirms the local pilot can see the endpoint
  # registration and generates correct RDS and SDS response.
  echo "Service discovery: "
  curl 'http://localhost:8080/v1/registration/test-proxy.default.svc.cluster.local|http'

  echo "Route discovery: "
  curl 'http://localhost:8080/v1/routes/9999/istio-proxy/sidecar~10.23.253.238~test-proxy.default~cluster.local'

  # All together: envoy using proxy and RDS for the test service, and SDS to route to the cluster on the local
  # machine.
  curl -x http://127.0.0.1:15002 http://test-proxy.default.svc.cluster.local:9999

}

# Tests require an existing kube api server, and a valid $HOME/.kube/config file
function run_tests() {
  test_standalone
}

if [[ ${1:-} == "start" ]] ; then
  build_all
  # Stop any previously running servers
  kill_all
  start_all
elif [[ ${1:-} == "stop" ]] ; then
  kill_all
elif [[ ${1:-} == "test" ]] ; then
  run_tests
else
  build_all
  # Stop any previously running servers
  kill_all
  start_all
  run_tests
  kill_all
fi
