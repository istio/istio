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
    echo "  cloud         cloud provider name (required)"
    echo
    echo "  e.g. ./test_crossgrade.sh \"
    echo "        --from_hub=gcr.io/istio-testing --from_tag=d639408fd --from_path=/tmp/release-d639408fd \"
    echo "        --to_hub=gcr.io/istio-release --to_tag=1.0.2 --to_path=/tmp/istio-1.0.2 --cloud=GKE"
    echo
    exit 1
}

ISTIO_NAMESPACE="istio-system"
# Minimum % of all requests that must be 200 for test to pass.
MIN_200_PCT_FOR_PASS="85"

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
        --cloud)
            CLOUD=${VALUE}
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

echo "Testing crossgrade from ${FROM_HUB}:${FROM_TAG} at ${FROM_PATH} to ${TO_HUB}:${TO_TAG} at ${TO_PATH} in namespace ${ISTIO_NAMESPACE}, auth=${AUTH_ENABLE}, cleanup=${SKIP_CLEANUP}"

ISTIO_ROOT=${GOPATH}/src/istio.io/istio
TMP_DIR=/tmp/istio_upgrade_test
LOCAL_FORTIO_LOG=${TMP_DIR}/fortio_local.log
POD_FORTIO_LOG=${TMP_DIR}/fortio_pod.log

# Make sure to change templates/*.yaml with the correct address if this changes.
TEST_NAMESPACE="test"

# This must be at least as long as the script execution time.
# Edit fortio-cli.yaml to the same value when changing this.
TRAFFIC_RUNTIME_SEC=500

# Used to signal that background external process is done.
EXTERNAL_FORTIO_DONE_FILE=${TMP_DIR}/fortio_done_file

echo_and_run() { echo "# RUNNING $*" ; "$@" ; }
echo_and_run_quiet() { echo "# RUNNING(quiet) $*" ; "$@" > /dev/null 2>&1 ; }
echo_and_run_or_die() { echo "# RUNNING $*" ; "$@" || die "failed!" ; }

# withRetries retries the given command ${1} times with ${2} sleep between retries
# e.g. withRetries 10 60 myFunc param1 param2
#   runs "myFunc param1 param2" up to 10 times with 60 sec sleep in between.
withRetries() {
    local max_retries=${1}
    local sleep_sec=${2}
    local n=0
    shift
    shift
    while (( n < max_retries )); do
      echo "RUNNING $*" ; "${@}" && break
      echo "Failed, sleeping ${sleep_sec} seconds and retrying..."
      ((n++))
      sleep "${sleep_sec}"
    done

    if (( n == max_retries )); then die "$* failed after retrying ${max_retries} times."; fi
    echo "Succeeded."
}

# withRetriesMaxTime retries the given command repeatedly with ${2} sleep between retries until ${1} seconds have elapsed.
# e.g. withRetries 300 60 myFunc param1 param2
#   runs "myFunc param1 param2" for up 300 seconds with 60 sec sleep in between.
withRetriesMaxTime() {
    local total_time_max=${1}
    local sleep_sec=${2}
    local start_time=${SECONDS}
    shift
    shift
    while (( SECONDS - start_time <  total_time_max )); do
      echo "RUNNING $*" ; "${@}" && break
      echo "Failed, sleeping ${sleep_sec} seconds and retrying..."
      sleep "${sleep_sec}"
    done

    if (( SECONDS - start_time >=  total_time_max )); then die "$* failed after retrying for ${total_time_max} seconds."; fi
    echo "Succeeded."
}

# checkIfDeleted checks if a resource has been deleted, returns 1 if it has not.
# e.g. checkIfDeleted ConfigMap my-config-map istio-system
#   OR checkIfDeleted namespace istio-system
checkIfDeleted() {
    local resp
    if [ -n "${3}" ]; then
        resp=$( kubectl get "${1}" -n "${3}" "${2}" 2>&1 )
    else
        resp=$( kubectl get "${1}" "${2}" 2>&1 )
    fi
    if [[ "${resp}" == *"Error from server (NotFound)"* ]]; then
        return 0
    fi
    echo "Response from server for kubectl get: "
    echo "${resp}"
    return 1
}

deleteWithWait() {
    # Don't complain if resource is already deleted.
    echo_and_run_quiet kubectl delete "${1}" -n "${3}" "${2}"
    withRetries 60 10 checkIfDeleted "${1}" "${2}" "${3}"
}

installIstioSystemAtVersionHelmTemplate() {
    writeMsg "helm templating then applying new yaml using version ${2} from ${3}."
    if [ -n "${AUTH_ENABLE}" ]; then
        echo "Auth is enabled, generating manifest with auth."
        auth_opts="--set global.mtls.enabled=true --set global.controlPlaneSecurityEnabled=true "
    fi
    release_path="${3}"/install/kubernetes/helm/istio
    if [[ "${release_path}" == *"1.1"* || "${release_path}" == *"master"* ]]; then
        # See https://preliminary.istio.io/docs/setup/kubernetes/helm-install/
        helm init --client-only
        for i in install/kubernetes/helm/istio-init/files/crd*yaml; do
            echo_and_run kubectl apply -f "${i}"
        done
        sleep 5 # Per official Istio documentation!

        helm template "${release_path}" "${auth_opts}" \
        --name istio --namespace "${ISTIO_NAMESPACE}" \
        --set gateways.istio-ingressgateway.autoscaleMin=4 \
        --set pilot.autoscaleMin=2 \
        --set mixer.telemetry.autoscaleMin=2 \
        --set mixer.policy.autoscaleMin=2 \
        --set prometheus.enabled=false \
        --set global.hub="${1}" \
        --set global.tag="${2}" \
        --set global.defaultPodDisruptionBudget.enabled=true > "${ISTIO_ROOT}/istio.yaml" || die "helm template failed"
    else
        helm template "${release_path}" "${auth_opts}" \
        --name istio --namespace "${ISTIO_NAMESPACE}" \
        --set gateways.istio-ingressgateway.autoscaleMin=4 \
        --set pilot.autoscaleMin=2 \
        --set mixer.istio-telemetry.autoscaleMin=2 \
        --set mixer.istio-policy.autoscaleMin=2 \
        --set prometheus.enabled=false \
        --set global.hub="${1}" \
        --set global.tag="${2}" > "${ISTIO_ROOT}/istio.yaml" || die "helm template failed"
    fi


    withRetries 3 60 kubectl apply -f "${ISTIO_ROOT}"/istio.yaml
}

installTest() {
   writeMsg "Installing test deployments"
   kubectl apply -n "${TEST_NAMESPACE}" -f "${TMP_DIR}/gateway.yaml" || die "kubectl apply gateway.yaml failed"
   # We don't want to auto-inject into ${ISTIO_NAMESPACE}, so echosrv and load client must be in different namespace.
   kubectl apply -n "${TEST_NAMESPACE}" -f "${TMP_DIR}/fortio.yaml" || die "kubectl apply fortio.yaml failed"
   sleep 10
}

# Sends traffic from internal pod (Fortio load command) to Fortio echosrv.
# Since this may block for some time due to restarts, it should be run in the background. Use waitForJob to check for
# completion.
_sendInternalRequestTraffic() {
    local job_name=cli-fortio
    deleteWithWait job "${job_name}" "${TEST_NAMESPACE}"
    start_time=${SECONDS}
    withRetries 10 60 kubectl apply -n "${TEST_NAMESPACE}" -f "${TMP_DIR}/fortio-cli.yaml"
    waitForJob "${job_name}"
    # Any timeouts typically occur in the first 20s
    if (( SECONDS - start_time < 100 )); then
        echo "${job_name} failed"
        return 1
    fi
}
sendInternalRequestTraffic() {
    writeMsg "Sending internal traffic"
    withRetries 10 0 _sendInternalRequestTraffic
}

# Runs traffic from external fortio client, with retries.
runFortioLoadCommand() {
    withRetries 10 10  echo_and_run fortio load -c 32 -t "${TRAFFIC_RUNTIME_SEC}"s -qps 10 \
        -H "Host:echosrv.test.svc.cluster.local" "http://${1}/echo?size=200" &> "${LOCAL_FORTIO_LOG}"
    echo "done" >> "${EXTERNAL_FORTIO_DONE_FILE}"
}

waitForExternalRequestTraffic() {
    echo "Waiting for external traffic to complete"
    while [ ! -f "${EXTERNAL_FORTIO_DONE_FILE}" ]; do
        sleep 10
    done
}

# Sends external traffic from machine test is running on to Fortio echosrv through external IP and ingress gateway LB.
sendExternalRequestTraffic() {
    writeMsg "Sending external traffic"
    runFortioLoadCommand "${1}" &
}

restartDataPlane() {
    # Apply label within deployment spec.
    # This is a hack to force a rolling restart without making any material changes to spec.
    writeMsg "Restarting deployment ${1}, patching label to force restart."
    echo_and_run_or_die kubectl patch deployment "${1}" -n "${TEST_NAMESPACE}" -p'{"spec":{"template":{"spec":{"containers":[{"name":"echosrv","env":[{"name":"RESTART_'"$(date +%s)"'","value":"1"}]}]}}}}'
}

resetConfigMap() {
    deleteWithWait ConfigMap "${1}" "${ISTIO_NAMESPACE}"
    kubectl create -n "${ISTIO_NAMESPACE}" -f "${2}"
}

writeMsg() {
    printf "\\n\\n****************\\n\\n%s\\n\\n****************\\n\\n" "${1}"
}

_waitForIngress() {
    INGRESS_HOST=$(kubectl -n "${ISTIO_NAMESPACE}" get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    INGRESS_PORT=$(kubectl -n "${ISTIO_NAMESPACE}" get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http2")].port}')
    INGRESS_ADDR=${INGRESS_HOST}:${INGRESS_PORT}
    if [ -z "${INGRESS_HOST}" ]; then return 1; fi
}

waitForIngress() {
    echo "Waiting for ingress-gateway addr..."
    withRetriesMaxTime 300 10 _waitForIngress
    echo "Got ingress-gateway addr: ${INGRESS_ADDR}"
}


_waitForPodsReady() {
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
        return 0
    fi

    echo "${pods_str}"
    return 1
}

waitForPodsReady() {
    echo "Waiting for pods to be ready in ${1}..."
    withRetriesMaxTime 600 10 _waitForPodsReady "${1}"
    echo "All pods ready."
}

_checkEchosrv() {
    resp=$( curl -o /dev/null -s -w "%{http_code}\\n" -HHost:echosrv.${TEST_NAMESPACE}.svc.cluster.local "http://${INGRESS_ADDR}/echo" || echo $? )
    if [[ "${resp}" = *"200"* ]]; then
        echo "Got correct response from echosrv."
        return 0
    fi
    echo "Got bad echosrv response: ${resp}"
    return 1
}

checkEchosrv() {
    writeMsg "Checking echosrv..."
    withRetriesMaxTime 300 10 _checkEchosrv
}

waitForJob() {
    echo "Waiting for job ${1} to complete..."
    local start_time=${SECONDS}
    until kubectl get jobs -n "${TEST_NAMESPACE}" "${1}" -o jsonpath='{.status.conditions[?(@.type=="Complete")].status}' | grep True ; do
        sleep 1 ;
    done
    run_time=0
    (( run_time = SECONDS - start_time ))
    echo "Job ${1} ran for ${run_time} seconds."
}

resetCluster() {
    echo "Cleaning cluster by removing namespaces ${ISTIO_NAMESPACE} and ${TEST_NAMESPACE}"
    deleteWithWait namespace "${ISTIO_NAMESPACE}"
    deleteWithWait namespace "${TEST_NAMESPACE}"
    echo "All namespaces deleted. Recreating ${ISTIO_NAMESPACE} and ${TEST_NAMESPACE}"

    echo_and_run_or_die kubectl create namespace "${ISTIO_NAMESPACE}"
    echo_and_run_or_die kubectl create namespace "${TEST_NAMESPACE}"
    echo_and_run_or_die kubectl label namespace "${TEST_NAMESPACE}" istio-injection=enabled
}

# Returns 0 if the passed string has form "Code 200 : 6601 (94.6 %)" and the percentage is smaller than ${MIN_200_PCT_FOR_PASS}
percent200sAbove() {
    local s=$1
    local regex="Code 200 : [0-9]+ \\(([0-9]+)\\.[0-9]+ %\\)"
    if [[ $s =~ $regex ]]; then
        local pct200s="${BASH_REMATCH[1]}"
        if (( pct200s > MIN_200_PCT_FOR_PASS )); then
            return 0
        fi
        return 1
    fi
    echo "No Code 200 percentage found in log."
    return 1
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

# create cluster admin role binding
user="cluster-admin"
if [[ $CLOUD == "GKE" ]];then
  user="$(gcloud config get-value core/account)"
fi
kubectl create clusterrolebinding cluster-admin-binding --clusterrole=cluster-admin --user="${user}" || echo "clusterrolebinding already created."


echo_and_run pushd "${ISTIO_ROOT}"

resetCluster

installIstioSystemAtVersionHelmTemplate "${FROM_HUB}" "${FROM_TAG}" "${FROM_PATH}"
waitForIngress
waitForPodsReady "${ISTIO_NAMESPACE}"

# Make a copy of the "from" sidecar injector ConfigMap so we can restore the sidecar independently later.
echo_and_run kubectl get ConfigMap -n "${ISTIO_NAMESPACE}" istio-sidecar-injector -o yaml > ${TMP_DIR}/sidecar-injector-configmap.yaml

installTest
waitForPodsReady "${TEST_NAMESPACE}"
checkEchosrv

# Run internal traffic in the background since we may have to relaunch it if the job fails.
sendInternalRequestTraffic &
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
resetConfigMap istio-sidecar-injector "${TMP_DIR}"/sidecar-injector-configmap.yaml
restartDataPlane echosrv-deployment-v1
sleep 140


installIstioSystemAtVersionHelmTemplate "${FROM_HUB}" "${FROM_TAG}" "${FROM_PATH}"
waitForPodsReady "${ISTIO_NAMESPACE}"

echo "Test ran for ${SECONDS} seconds."
if (( SECONDS > TRAFFIC_RUNTIME_SEC )); then
    echo "WARNING: test duration was ${SECONDS} but traffic only ran for ${TRAFFIC_RUNTIME_SEC}"
fi

cli_pod_name=$(kubectl -n "${TEST_NAMESPACE}" get pods -lapp=cli-fortio -o jsonpath='{.items[0].metadata.name}')
echo "Traffic client pod is ${cli_pod_name}, waiting for traffic to complete..."
waitForJob cli-fortio
kubectl logs -f -n "${TEST_NAMESPACE}" -c echosrv "${cli_pod_name}" &> "${POD_FORTIO_LOG}" || echo "Could not find ${cli_pod_name}"
waitForExternalRequestTraffic

local_log_str=$(grep "Code 200" "${LOCAL_FORTIO_LOG}")
pod_log_str=$(grep "Code 200"  "${POD_FORTIO_LOG}")

# First dump local log
cat ${LOCAL_FORTIO_LOG}
if [[ ${local_log_str} != *"Code 200"* ]];then
    echo "=== No Code 200 found in external traffic log ==="
    failed=true
elif ! percent200sAbove "${local_log_str}"; then
    echo "=== Errors found in external traffic exceeded ${MIN_200_PCT_FOR_PASS}% threshold ==="
    failed=true
else
    echo "=== Errors found in external traffic is within ${MIN_200_PCT_FOR_PASS}% threshold ==="
fi

# Then dump pod log
cat ${POD_FORTIO_LOG}
if [[ ${pod_log_str}  != *"Code 200"* ]]; then
    echo "=== No Code 200 found in internal traffic log ==="
    failed=true
elif ! percent200sAbove "${pod_log_str}"; then
    echo "=== Errors found in internal traffic exceeded ${MIN_200_PCT_FOR_PASS}% threshold ==="
    failed=true
else
    echo "=== Errors found in internal traffic is within ${MIN_200_PCT_FOR_PASS}% threshold ==="
fi

echo_and_run popd

if [ -n "${failed}" ]; then
    exit 1
fi

echo "SUCCESS"
