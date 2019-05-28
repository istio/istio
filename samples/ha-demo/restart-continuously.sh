#!/bin/bash
set -eu
NAMESPACE=${NAMESPACE:-"ha-demo"}

function update_deployments() {
    CTIME=$(date +%s)
    kubectl set env deployments --all  LAST_MANUAL_RESTART="${CTIME}" --namespace=${NAMESPACE} > /dev/null 
}

function wait_for_deployments() {
    for depl in $(kubectl get deployments. -n ha-demo --no-headers -o custom-columns=NAME:.metadata.name)
    do
      until kubectl rollout status deployment/$depl --namespace=${NAMESPACE} > /dev/null; do
        sleep 0.5
      done
    done

    sleep 1
}

echo -n Restarting pods
while true ; do
    update_deployments
    echo -n .
    wait_for_deployments
done
