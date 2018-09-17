#!/bin/bash

# This test checks for control and data plane upgrade from the passed in version to $HUB/$TAG.
# It runs the following steps:
# 1. Installs istio with multiple gateway replicas at source version. Version is defined by tag e.g. 1.0.0
# 2. Installs fortio echosrv with a couple of different subsets/destination rules with multiple replicas.
# 3. Sends external traffic to echosrv through ingress gateway.
# 4. Sends internal traffic to echosrv from fortio load pod.
# 5. Upgrades control plane to $HUB/$TAG. *** Ensure these are available or upgrade will fail! ***
# 6. Does a rolling restart of one of the echosrv subsets.
# 7. Parses the output from the load pod to check for any errors during upgrade and returns 1 is any are found.
#
# Note 1: in order to generate the correct manifest, the script must git checkout the source and target release tags.
#         This will fail if the workspace is not clean.
# Note 2: assumes clean cluster with nothing installed. Try kubectl delete namespace --now istio-system test.

usage() {
    echo "Usage:"
    echo "  ./test_upgrade_from.sh <from tag>" 
    echo
    echo '  e.g. ./test_upgrade_from.sh 1.0.0 (target is $HUB/$TAG)'
    echo
    exit 1
}

if [[ -z "${1}" ]]; then
    usage
fi

FROM_TAG=${1}
FROM_HUB=gcr.io/istio-release

ISTIO_ROOT=${GOPATH}/src/istio.io/istio
LOCAL_FORTIO_LOG=/tmp/fortio_local.log
POD_FORTIO_LOG=/tmp/fortio_pod.log

# Make sure to change templates/*.yaml with the correct address if this changes.
TEST_NAMESPACE=test

original_branch=`git rev-parse --abbrev-ref HEAD`
cur_branch=${original_branch}
cur_sha=`git rev-parse HEAD`

installIstioSystemAtVersionHelmTemplate() {
   helm template ${ISTIO_ROOT}/install/kubernetes/helm/istio \
    --name istio --namespace istio-system \
    --set gateways.istio-ingressgateway.replicaCount=4 \
    --set gateways.istio-ingressgateway.autoscaleMin=4 \
    --set global.hub=${1} \
    --set global.tag=${2} > ${ISTIO_ROOT}/istio.yaml

   kubectl apply -n istio-system -f ${ISTIO_ROOT}/istio.yaml
}

gitChangeToTag() {
   original_branch=`git rev-parse --abbrev-ref HEAD`
   git checkout tags/${1} || die "Cannot git checkout, ensure your workspace is clean."
   cur_branch=`git rev-parse --abbrev-ref HEAD`
   cur_sha=`git rev-parse HEAD`
   writeMsg "Switching branch from ${original_branch} to tags/${1}"
}

gitRestorePrevBranch() {
   git checkout ${original_branch}
   cur_branch=`git rev-parse --abbrev-ref HEAD`
   cur_sha=`git rev-parse HEAD`
   writeMsg "Switching branch to ${original_branch}"
}

installIstioSystemAtTag() {
   gitChangeToTag ${2}
   installIstioSystemAtVersionHelmTemplate ${1} ${2}
   gitRestorePrevBranch
}

installTest() {
    writeMsg "Installing test deployments"
   kubectl apply -n ${TEST_NAMESPACE} -f ${ISTIO_ROOT}/tests/upgrade/templates/gateway.yaml
   # We don't want to auto-inject into istio-system, so echosrv and load client must be in different namespace.
   kubectl apply -n ${TEST_NAMESPACE} -f ${ISTIO_ROOT}/tests/upgrade/templates/fortio.yaml
   sleep 10
}

# Sends traffic from internal pod (Fortio load command) to Fortio echosrv.
sendInternalRequestTraffic() {
   writeMsg "Sending internal traffic"
   kubectl apply -n ${TEST_NAMESPACE} -f ${ISTIO_ROOT}/tests/upgrade/templates/fortio-cli.yaml
}

# Sends external traffic from machine test is running on to Fortio echosrv through external IP and ingress gateway LB.
sendExternalRequestTraffic() {
    writeMsg "Sending external traffic"
    fortio load -c 32 -t 300s -qps 10 -H "Host:echosrv.test.svc.cluster.local" http://${1}/echo?size=200 &> ${LOCAL_FORTIO_LOG}
}

restartDataPlane() {
    # Apply label within deployment spec.
    # This is a hack to force a rolling restart without making any material changes to spec.
    cur_time=$(date +%s)
    writeMsg "Restarting deployment ${1}"
    kubectl patch deployment ${1} -n ${TEST_NAMESPACE} -p'{"spec":{"template":{"spec":{"containers":[{"name":"echosrv","env":[{"name":"RESTART_","value":"1"}]}]}}}}'
}

writeMsg() {
    printf "\n\n****************\n\n${1}\n\n****************\n\n"

}

waitForIngress() {
    sleep_time_sec=10
    INGRESS_HOST=""
    while [ -z ${INGRESS_HOST} ]; do
        echo "Waiting for ingress-gateway addr..."
        INGRESS_HOST=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
        INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].port}')
        INGRESS_ADDR=${INGRESS_HOST}:${INGRESS_PORT}
        sleep ${sleep_time_sec}
        (( sleep_time_sofar += $sleep_time_sec ))
        if (( sleep_time_sofar > 300 )); then
            echo "Timed out waiting for ingress-gateway address"
            exit 1
        fi
    done
    echo "Got ingress-gateway addr: ${INGRESS_ADDR}"
}


waitForPodsReady() {
    sleep_time_sec=10
    while :; do
        echo "Waiting for pods to be ready in ${1}..."
        pods_str=$(kubectl -n ${1} get pods | tail -n +2 )
        arr=()
        while read -r line; do
           arr+=("$line")
        done <<< "$pods_str"

        ready="true"
        for line in "${arr[@]}"; do
            if [[ ${line} != *"Running"* ]]; then
                ready="false"
            fi
        done
        if [  "${ready}" = "true" ]; then
            echo "All pods ready."
            return
        fi

        sleep ${sleep_time_sec}
        (( sleep_time_sofar += $sleep_time_sec ))
        if (( sleep_time_sofar > 300 )); then
            echo "Timed out waiting for pods to be ready."
            echo ${pods_str}
            exit 1
        fi
    done
}

checkEchosrv() {
    sleep_time_sec=10
    while :; do
        resp=$( curl -o /dev/null -s -w "%{http_code}\n" -HHost:echosrv.${TEST_NAMESPACE}.svc.cluster.local http://${INGRESS_ADDR}/echo )
        if [[ "${resp}" = *"200"* ]]; then
            echo "Got correct response from echosrv."
            return
        else
            echo "Got bad echosrv response: ${resp}"
        fi

        sleep ${sleep_time_sec}
        (( sleep_time_sofar += $sleep_time_sec ))
        if (( sleep_time_sofar > 300 )); then
            echo "Timed out waiting for 200 from echosrv."
            exit 1
        fi
    done
}

git fetch upstream --tags --prune
git fetch origin --tags --prune

kubectl create namespace istio-system
kubectl create namespace ${TEST_NAMESPACE}
kubectl label namespace ${TEST_NAMESPACE} istio-injection=enabled

pushd ${ISTIO_ROOT}

installIstioSystemAtTag ${FROM_HUB} ${FROM_TAG}
waitForIngress
waitForPodsReady istio-system

installTest
waitForPodsReady ${TEST_NAMESPACE}
checkEchosrv

sendInternalRequestTraffic
sendExternalRequestTraffic ${INGRESS_ADDR}
# Let traffic clients establish all connections. There's some small startup delay, this covers it.
sleep 20
installIstioSystemAtVersionHelmTemplate ${HUB} ${TAG}
waitForPodsReady istio-system
# In principle it should be possible to restart data plane immediately, but being conservative here.
sleep 60
restartDataPlane echosrv-deployment-v1
# No way to tell when rolling restart completes because it's async. Make sure this is long enough to cover all the
# pods in the deployment at the minReadySeconds setting (should be > num pods x minReadySeconds + few extra seconds).
sleep 100

cli_pod_name=$(kubectl -n ${TEST_NAMESPACE} get pods -lapp=cli-fortio -o jsonpath='{.items[0].metadata.name}')
echo "Traffic client pod is ${cli_pod_name}, waiting for traffic to complete..."
kubectl logs -f -n ${TEST_NAMESPACE} -c echosrv ${cli_pod_name} &> ${POD_FORTIO_LOG}

local_log_str=$(grep "Code 200" ${LOCAL_FORTIO_LOG})
pod_log_str=$(grep "Code 200"  ${POD_FORTIO_LOG})

if [[ ${local_log_str} == "" ]]; then
    echo "No Code 200 found in external traffic log"
elif [[ ${local_log_str} != *"(100.0"* ]]; then
    printf "\n\nErrors found in external traffic log:\n\n"
    cat ${LOCAL_FORTIO_LOG}
fi

if [[ ${pod_log_str} == "" ]]; then
    echo "No Code 200 found in internal traffic log"
elif [[ ${pod_log_str} != *"(100.0"* ]]; then
    printf "\n\nErrors found in internal traffic log:\n\n"
    cat ${POD_FORTIO_LOG}
    exit 1
fi

popd

echo "SUCCESS"
