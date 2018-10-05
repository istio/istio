#!/bin/bash

# This test checks for control and data plane crossgrade. It runs the following steps:
# 1. Installs istio with multiple gateway replicas at from_hub/tag/path (path must point to a dir with release).
# 2. Installs fortio echosrv with a couple of different subsets/destination rules with multiple replicas.
# 3. Sends external traffic to echosrv through ingress gateway.
# 4. Sends internal traffic to echosrv from fortio load pod.
# 5. Upgrades control plane to to_hub/tag/path.
# 6. Does rolling restart of one of the echosrv subsets, which auto-injects upgraded version of sidecar.
# 7. Waits a while, then does downgrade.
# 8. Downgrades data plane by applying a saved ConfigMap from the previous version and doing rolling restart.
# 9. Downgrades control plane to from_hub/tag/path.
# 10. Parses the output from the load pod to check for any errors during upgrade and returns 1 is any are found.
#
# Dependencies that must be preinstalled: helm, fortio.
#

set -o errexit
set -o pipefail

command -v helm >/dev/null 2>&1 || { echo >&2 "helm must be installed, aborting."; exit 1; }
command -v fortio >/dev/null 2>&1 || { echo >&2 "fortio must be installed, aborting."; exit 1; }

usage() {
    echo "Usage:"
    echo "  ./test_crossgrade.sh [OPTIONS]"
    echo
    echo "  from_hub      hub of release to upgrade from (required)."
    echo "  from_tag      tag of release to upgrade from (required)."
    echo "  from_path     path to release dir to upgrade from (required)."
    echo "  to_hub        hub of release to upgrade to (required)."
    echo "  to_tag        tag of release to upgrade to (required)."
    echo "  to_path       path to release to upgrade to (required)."
    echo "  auth_enable   enable mtls."
    echo "  skip_cleanup  leave install intact after test completes."
    echo "  namespace     namespace to install istio control plane in (default istio-system)."
    echo
    echo "  e.g. ./test_crossgrade.sh \"
    echo "        --from_hub=gcr.io/istio-testing --from_tag=d639408fd --from_path=/tmp/release-d639408fd \"
    echo "        --to_hub=gcr.io/istio-release --to_tag=1.0.2 --to_path=/tmp/istio-1.0.2 --rollback"
    echo
    echo "   (tests whether it's possible to rollback from release-d639408fd under test back to official point release)"
    exit 1
}

ISTIO_NAMESPACE="istio-system"

while (( "$#" )); do
    PARAM=$(echo "${1}" | awk -F= '{print $1}')
    eval VALUE="$(echo "${1}" | awk -F= '{print $2}')"
    case "${PARAM}" in
        -h | --help)
            usage
            exit
            ;;
        --namespace)
            ISTIO_NAMESPACE=${VALUE}
            ;;
        --skip_cleanup)
            SKIP_CLEANUP=true
            ;;
        --auth_enable)
            AUTH_ENABLE=true
            ;;
         --rollback)
            ROLLBACK=true
            ;;
        --from_hub)
            FROM_HUB=${VALUE}
            ;;
        --from_tag)
            FROM_TAG=${VALUE}
            ;;
        --from_path)
            FROM_PATH=${VALUE}
            ;;
        --to_hub)
            TO_HUB=${VALUE}
            ;;
        --to_tag)
            TO_TAG=${VALUE}
            ;;
        --to_path)
            TO_PATH=${VALUE}
            ;;
        *)
            echo "ERROR: unknown parameter \"$PARAM\""
            usage
            exit 1
            ;;
    esac
    shift
done

if [[ -z "${FROM_HUB}" || -z "${FROM_TAG}" || -z "${FROM_PATH}" || -z "${TO_HUB}" || -z "${TO_TAG}" || -z "${TO_PATH}" ]]; then
    echo "Error: from_hub, from_tag, from_path, to_hub, to_tag, to_path must all be set."
    exit 1
fi

echo "Testing crossgrade from ${FROM_HUB}:${FROM_TAG} at ${FROM_PATH} to ${TO_HUB}:${TO_TAG} at ${TO_PATH} in namespace ${ISTIO_NAMESPACE}, auth=${AUTH_ENABLE}, cleanup=${SKIP_CLEANUP}, rollback=${ROLLBACK}"

ISTIO_ROOT=${GOPATH}/src/istio.io/istio
TMP_DIR=/tmp/istio_upgrade_test
LOCAL_FORTIO_LOG=${TMP_DIR}/fortio_local.log
POD_FORTIO_LOG=${TMP_DIR}/fortio_pod.log

# Make sure to change templates/*.yaml with the correct address if this changes.
TEST_NAMESPACE="test"

# This must be at least as long as the script execution time.
# Edit fortio-cli.yaml to the same value when changing this.
TRAFFIC_RUNTIME_SEC=700

echo_and_run() { echo "RUNNING $*" ; "$@" ; }

installIstioSystemAtVersionHelmTemplate() {
    writeMsg "helm installing version ${2} from ${3}."
    if [ -n "${AUTH_ENABLE}" ]; then
        echo "Auth is enabled, generating manifest with auth."
        auth_opts="--set global.mtls.enabled=true --set global.controlPlaneSecurityEnabled=true "
    fi
    release_path="${3}"/install/kubernetes/helm/istio
    helm template "${release_path}" "${auth_opts}" \
    --name istio --namespace "${ISTIO_NAMESPACE}" \
    --set gateways.istio-ingressgateway.replicaCount=4 \
    --set gateways.istio-ingressgateway.autoscaleMin=4 \
    --set global.hub="${1}" \
    --set global.tag="${2}" > "${ISTIO_ROOT}/istio.yaml"

   kubectl apply -n "${ISTIO_NAMESPACE}" -f "${ISTIO_ROOT}"/istio.yaml
}

installTest() {
   writeMsg "Installing test deployments"
   kubectl apply -n "${TEST_NAMESPACE}" -f "${TMP_DIR}/gateway.yaml"
   # We don't want to auto-inject into ${ISTIO_NAMESPACE}, so echosrv and load client must be in different namespace.
   kubectl apply -n "${TEST_NAMESPACE}" -f "${TMP_DIR}/fortio.yaml"
   sleep 10
}

# Sends traffic from internal pod (Fortio load command) to Fortio echosrv.
sendInternalRequestTraffic() {
   writeMsg "Sending internal traffic"
   kubectl apply -n "${TEST_NAMESPACE}" -f "${TMP_DIR}/fortio-cli.yaml"
}

# Sends external traffic from machine test is running on to Fortio echosrv through external IP and ingress gateway LB.
sendExternalRequestTraffic() {
    writeMsg "Sending external traffic"
    echo_and_run fortio load -c 32 -t "${TRAFFIC_RUNTIME_SEC}"s -qps 10 -H "Host:echosrv.test.svc.cluster.local" "http://${1}/echo?size=200" &> "${LOCAL_FORTIO_LOG}" &
}

restartDataPlane() {
    # Apply label within deployment spec.
    # This is a hack to force a rolling restart without making any material changes to spec.
    writeMsg "Restarting deployment ${1}, patching label to force restart."
    echo_and_run kubectl patch deployment "${1}" -n "${TEST_NAMESPACE}" -p'{"spec":{"template":{"spec":{"containers":[{"name":"echosrv","env":[{"name":"RESTART_'$(date +%s)'","value":"1"}]}]}}}}'
}

writeMsg() {
    printf "\\n\\n****************\\n\\n%s\\n\\n****************\\n\\n" "${1}"
}

waitForIngress() {
    sleep_time_sec=10
    sleep_time_sofar=0
    INGRESS_HOST=""
    while [ -z ${INGRESS_HOST} ]; do
        echo "Waiting for ingress-gateway addr..."
        INGRESS_HOST=$(kubectl -n "${ISTIO_NAMESPACE}" get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
        INGRESS_PORT=$(kubectl -n "${ISTIO_NAMESPACE}" get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].port}')
        INGRESS_ADDR=${INGRESS_HOST}:${INGRESS_PORT}
        sleep ${sleep_time_sec}
        (( sleep_time_sofar += sleep_time_sec ))
        if (( sleep_time_sofar > 300 )); then
            echo "Timed out waiting for ingress-gateway address"
            exit 1
        fi
    done
    echo "Got ingress-gateway addr: ${INGRESS_ADDR}"
}


waitForPodsReady() {
    sleep_time_sec=10
    sleep_time_sofar=0
    while :; do
        echo "Waiting for pods to be ready in ${1}..."
        pods_str=$(kubectl -n "${1}" get pods | tail -n +2 )
        arr=()
        while read -r line; do
           arr+=("$line")
        done <<< "$pods_str"

        ready="true"
        for line in "${arr[@]}"; do
            if [[ ${line} != *"Running"* && ${line} != *"Completed"* ]]; then
                ready="false"
            fi
        done
        if [  "${ready}" = "true" ]; then
            echo "All pods ready."
            return
        fi

        sleep ${sleep_time_sec}
        (( sleep_time_sofar += sleep_time_sec ))
        if (( sleep_time_sofar > 300 )); then
            echo "Timed out waiting for pods to be ready."
            echo "${pods_str}"
            exit 1
        fi
    done
}

checkEchosrv() {
    sleep_time_sec=10
    sleep_time_sofar=0
    while :; do
        resp=$( curl -o /dev/null -s -w "%{http_code}\\n" -HHost:echosrv.${TEST_NAMESPACE}.svc.cluster.local "http://${INGRESS_ADDR}/echo" || echo $? )
        echo "${resp}"
        if [[ "${resp}" = *"200"* ]]; then
            echo "Got correct response from echosrv."
            return
        else
            echo "Got bad echosrv response: ${resp}"
        fi

        sleep ${sleep_time_sec}
        (( sleep_time_sofar += sleep_time_sec ))
        if (( sleep_time_sofar > 300 )); then
            echo "Timed out waiting for 200 from echosrv."
            exit 1
        fi
    done
}

resetNamespaces() {
    sleep_time_sec=10
    sleep_time_sofar=0
    kubectl delete namespace "${ISTIO_NAMESPACE}" "${TEST_NAMESPACE}" || echo "namespaces may already be deleted"
    while :; do
        resp=$( kubectl get namespaces )
        if [[ "${resp}" != *"Terminating"* ]]; then
            echo "All namespaces deleted."
            kubectl create namespace "${ISTIO_NAMESPACE}"
            kubectl create namespace "${TEST_NAMESPACE}"
            kubectl label namespace "${TEST_NAMESPACE}" istio-injection=enabled
            return
        else
            echo "Waiting for namespaces ${ISTIO_NAMESPACE} and ${TEST_NAMESPACE} to be deleted."
        fi

        sleep ${sleep_time_sec}
        (( sleep_time_sofar += sleep_time_sec ))
        if (( sleep_time_sofar > 300 )); then
            echo "Timed out waiting for 200 from echosrv."
            exit 1
        fi
    done
}

die() {
    echo "$*" 1>&2 ; exit 1;
}

# Make a copy of test manifests in case either to/from branch doesn't contain them.
copy_test_files() {
    rm -Rf ${TMP_DIR}
    mkdir -p ${TMP_DIR}
    cp -f "${ISTIO_ROOT}"/tests/upgrade/templates/* "${TMP_DIR}"/.
}

copy_test_files

kubectl create clusterrolebinding cluster-admin-binding --clusterrole=cluster-admin --user="$(gcloud config get-value core/account)" || echo "clusterrolebinding already created."

pushd "${ISTIO_ROOT}" || exit 1

resetNamespaces

# In rollback case, grab the sidecar injector ConfigMap from the target version so we can upgrade the dataplane to it
# first, before switching the control plane.
if [ -n "${ROLLBACK}" ]; then
    writeMsg "Temporarily installing target version to extract sidecar-injector ConfigMap."
    installIstioSystemAtVersionHelmTemplate "${TO_HUB}" "${TO_TAG}" "${TO_PATH}"
    waitForPodsReady "${ISTIO_NAMESPACE}"
    kubectl get ConfigMap -n "${ISTIO_NAMESPACE}" istio-sidecar-injector -o yaml > ${TMP_DIR}/sidecar-injector-configmap.yaml
    resetNamespaces
    echo "ConfigMap extracted."
fi

installIstioSystemAtVersionHelmTemplate "${FROM_HUB}" "${FROM_TAG}" "${FROM_PATH}"
waitForIngress
waitForPodsReady "${ISTIO_NAMESPACE}"

# Make a copy of the "from" sidecar injector ConfigMap so we can restore the sidecar independently later.
kubectl get ConfigMap -n "${ISTIO_NAMESPACE}" istio-sidecar-injector -o yaml > ${TMP_DIR}/sidecar-injector-configmap.yaml

installTest
waitForPodsReady "${TEST_NAMESPACE}"
checkEchosrv

sendInternalRequestTraffic
sendExternalRequestTraffic "${INGRESS_ADDR}"
# Let traffic clients establish all connections. There's some small startup delay, this covers it.
echo "Waiting for traffic to settle..."
sleep 20

installIstioSystemAtVersionHelmTemplate "${TO_HUB}" "${TO_TAG}" "${TO_PATH}"
waitForPodsReady "${ISTIO_NAMESPACE}"
# In principle it should be possible to restart data plane immediately, but being conservative here.
sleep 60

restartDataPlane echosrv-deployment-v1
# No way to tell when rolling restart completes because it's async. Make sure this is long enough to cover all the
# pods in the deployment at the minReadySeconds setting (should be > num pods x minReadySeconds + few extra seconds).
sleep 140

# Now do a rollback. In a rollback, we update the data plane first.
writeMsg "Starting rollback - first, rolling back data plane to ${TO_PATH}"
kubectl delete ConfigMap -n "${ISTIO_NAMESPACE}" istio-sidecar-injector
sleep 5
kubectl create -n "${ISTIO_NAMESPACE}" -f "${TMP_DIR}"/sidecar-injector-configmap.yaml
restartDataPlane echosrv-deployment-v1
sleep 140

installIstioSystemAtVersionHelmTemplate "${FROM_HUB}" "${FROM_TAG}" "${FROM_PATH}"
waitForPodsReady "${ISTIO_NAMESPACE}"

echo "Test ran for ${SECONDS} seconds."
if (( SECONDS > TRAFFIC_RUNTIME_SEC )); then
    writeMsg "WARNING: test duration was ${SECONDS} but traffic only ran for ${TRAFFIC_RUNTIME_SEC}"
fi

cli_pod_name=$(kubectl -n "${TEST_NAMESPACE}" get pods -lapp=cli-fortio -o jsonpath='{.items[0].metadata.name}')
echo "Traffic client pod is ${cli_pod_name}, waiting for traffic to complete..."
kubectl logs -f -n "${TEST_NAMESPACE}" -c echosrv "${cli_pod_name}" &> "${POD_FORTIO_LOG}" || echo "Could not find ${cli_pod_name}"

local_log_str=$(grep "Code 200" "${LOCAL_FORTIO_LOG}")
pod_log_str=$(grep "Code 200"  "${POD_FORTIO_LOG}")

if [[ ${local_log_str} == "" ]]; then
    echo "No Code 200 found in external traffic log"
    failed=true
elif [[ ${local_log_str} != *"(100.0"* ]]; then
    printf "\\n\\nErrors found in external traffic log:\\n\\n"
    cat ${LOCAL_FORTIO_LOG}
fi

if [[ ${pod_log_str} == "" ]]; then
    echo "No Code 200 found in internal traffic log"
    failed=true
elif [[ ${pod_log_str} != *"(100.0"* ]]; then
    printf "\\n\\nErrors found in internal traffic log:\\n\\n"
    cat ${POD_FORTIO_LOG}
    exit 1
fi

popd || exit 1

if [ -n "${failed}" ]; then
    exit 1
fi

echo "SUCCESS"
