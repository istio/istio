#!/bin/bash
set -eu
NAMESPACE=${NAMESPACE:-"ha-demo"}

# TODO loop over all deployments
function update_deployments() {
    CTIME=$(date +%s)
    kubectl set env deployment/news  LAST_MANUAL_RESTART="${CTIME}" --namespace=${NAMESPACE} > /dev/null 
    kubectl set env deployment/article-v2  LAST_MANUAL_RESTART="${CTIME}" --namespace=${NAMESPACE} > /dev/null
}

function wait_for_deployments() {
    until kubectl rollout status deployment/news --namespace=${NAMESPACE} > /dev/null; do
      sleep 0.5
    done
    until kubectl rollout status deployment/article-v2 --namespace=${NAMESPACE} > /dev/null; do
      sleep 0.5
    done
    sleep 1
}

echo -n Restarting pods
while true ; do
    update_deployments
    echo -n .
    wait_for_deployments
done