#!/bin/bash

# Base directory definitions
BASE_DIR=/app
DATA_DIR=${BASE_DIR}/data

# The in-container port for the API Server to listen on. This needs to be exposed/mapped from the container.
APISERVER_PORT=8080

# Lifecycle check frequency and iterations. Even though the tests will try to do graceful cleanup of the
# containers, there is a good chance that they won't be able to do so. We don't want the containers to linger
# indefinitely.
#
# The script performs health checks at the frequency defined by LIFECYCLE_CHECK_FREQUENCY_SECS. After doing
# this for the number of iterations defined with LIFECYCLE_CHECK_ITERATIONS, the scripts will simply exit.
#
LIFECYCLE_CHECK_FREQUENCY_SECS=3
LIFECYCLE_CHECK_ITERATIONS=10

mkdir -p ${DATA_DIR}

# Start Etcd
bin/etcd --data-dir "${DATA_DIR}" &
STATUS=$?
echo "Etcd status: ${STATUS}"
if [[ "${STATUS}" -ne 0 ]]; then
  echo "Failed to start etcd: ${STATUS}"
  exit "${STATUS}"
fi

# Start Api Server
/bin/kube-apiserver --etcd-servers http://127.0.0.1:2379 \
        --insecure-port "${APISERVER_PORT}" \
        -v 2 \
        --insecure-bind-address 0.0.0.0 &
STATUS=$?
echo "Api Server status: ${STATUS}"
if [[ "${STATUS}" -ne 0 ]]; then
  echo "Failed to start api server: ${STATUS}"
  exit "${STATUS}"
fi

ITERATION="0"

while sleep "${LIFECYCLE_CHECK_FREQUENCY}"; do
  ps aux |grep etcd |grep -q -v grep
  PROCESS_1_STATUS=$?
  ps aux |grep kube-apiserver |grep -q -v grep
  PROCESS_2_STATUS=$?

  # If the greps above find anything, they exit with 0 status
  # If they are not both 0, then something is wrong
  if [ $PROCESS_1_STATUS -ne 0 -o $PROCESS_2_STATUS -ne 0 ]; then
    echo "One of the processes has already exited."
    exit 1
  fi

  # Don't run indefinitely. Exit after a set number of iterations.
  if [ "${ITERATION}" -eq "${LIFECYCLE_CHECK_ITERATIONS}" ]; then
    exit 2
  fi

  echo "Completed iteration: ${ITERATION}"
  let ITERATION++

done

