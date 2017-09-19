#!/bin/bash

# Helper script to start a local test environment.

ISTIO_BASE=${ISTIO_BASE:-${GOPATH:-${HOME}/go}}
ISTIO_IO=${ISTIO_IO:-${ISTIO_BASE}/src/istio.io}

LOG_DIR=${LOG_DIR:-${ISTIO_IO}/istio/bazel-testlogs/local}

# Stop locally running processes started with istio_local_start
function istio_local_kill() {
  if [[ -f $LOG_DIR/pilot.pid ]] ; then
    kill -9 $(cat $LOG_DIR/pilot.pid)
    kill -9 $(cat $LOG_DIR/mixer.pid)
    kill -9 $(cat $LOG_DIR/envoy.pid)
    kill -9 $(cat $LOG_DIR/test_server.pid)
  fi
}

# Start pilot, envoy and mixer for local integration testing.
# Requires a valid k8s config, like minikube.
function istio_local_start() {

  local IP=$(localIP)

  mkdir -p $LOG_DIR
  POD_NAME=pilot POD_NAMESPACE=default  ${ISTIO_IO}/pilot/bazel-bin/cmd/pilot-discovery/pilot-discovery discovery -n default --kubeconfig ~/.kube/config > $LOG_DIR/pilot.logs 2>&1&
  echo $! > $LOG_DIR/pilot.pid

  ${ISTIO_IO}/mixer/bazel-bin/cmd/server/mixs server \
    --configStoreURL=fs:${ISTIO_IO}/mixer/testdata/configroot \
    --configStore2URL=fs:${ISTIO_IO}/mixer/testdata/config \
    -v=2 --logtostderr > $LOG_DIR/mixer.logs 2>&1 &
  echo $! > $LOG_DIR/mixer.pid

  ${ISTIO_IO}/pilot/bazel-bin/test/server/server --port 9999 > $LOG_DIR/test_server.log 2>&1 &
  echo $! > $LOG_DIR/test_server.pid

  # Normal sidecar
  POD_NAME=localtest POD_NAMESPACE=default ${ISTIO_IO}/pilot/bazel-bin/cmd/pilot-agent/pilot-agent \
    --binaryPath ${ISTIO_IO}/proxy/bazel-bin/src/envoy/mixer/envoy \
    --configPath $LOG_DIR \
    --discoveryAddress localhost:8080 \
    --proxyAdminPort 15001 \
    > $LOG_DIR/sidecar.log 2>&1 &
  echo $! > $LOG_DIR/sidecar.pid

  # Envoy with 'lds' disabled, so we can use manual config.
  ${ISTIO_IO}/proxy/bazel-bin/src/envoy/mixer/envoy -c $ISTIO_IO/proxy/tools/deb/test/envoy_local.json --restart-epoch 1 \
    --drain-time-s 2 --parent-shutdown-time-s 3 --service-cluster istio-proxy \
    --service-node sidecar~$IP~mysvc.~svc.cluster.local > $LOG_DIR/envoy.log 2>&1 &
  echo $! > $LOG_DIR/envoy.pid
}

function localIP() {
    echo $(ip route get 8.8.8.8 | head -1 | cut -d' ' -f8)
}
