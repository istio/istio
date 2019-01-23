#!/bin/bash
#
# Sets up a cluster for perf testing - GCP/GKE
#   tools/setup_perf_cluster.sh
# Notes:
# * See README.md
# * Make sure istioctl in your path is the one matching your release/crd/...
# * You need to update istio-auth.yaml or run from a release directory:
#   source tools/setup_perf_cluster.sh
#   setup_all
# (inside google you may need to rerun setup_vm_firewall multiple times)
#
# This can be used as a script or sourced and functions called interactively
#
# The script must be run/sourced from the parent of the tools/ directory
#

PROJECT=${PROJECT:-$(gcloud config list --format 'value(core.project)' 2>/dev/null)}
ZONE=${ZONE:-us-east4-b}
CLUSTER_NAME=${CLUSTER_NAME:-istio-perf}
MACHINE_TYPE=${MACHINE_TYPE:-n1-highcpu-2}
NUM_NODES=${NUM_NODES:-6} # SvcA<->SvcB + Ingress + Pilot + Mixer + 1 extra (kube-system)
VM_NAME=${VM_NAME:-fortio-vm}
ISTIOCTL=${ISTIOCTL:-istioctl} # to override istioctl from outside of the path
FORTIO_NAMESPACE=${FORTIO_NAMESPACE:-fortio} # Namespace for non istio app
ISTIO_NAMESPACE=${ISTIO_NAMESPACE:-istio} # Namespace for istio injected app
# Should not be set to true for perf measurement but to troubleshoot the setup
DEBUG=false

function Usage() {
    echo "usage: PROJECT=project ZONE=zone $0"
    echo "also settable are NUM_NODES, MACHINE_TYPE, CLUSTER_NAME, VM_NAME, VM_IMAGE"
    exit 1
}

function abspath() {
# Source https://stackoverflow.com/questions/3915040/bash-fish-command-to-print-absolute-path-to-a-file
# Thanks to Alexander Klimetschek

    # generate absolute path from relative path
    # $1     : relative filename
    # return : absolute path
    if [ -d "$1" ]; then
        # dir
        (cd "$1"; pwd)
    elif [ -f "$1" ]; then
        # file
        if [[ $1 = /* ]]; then
            echo "$1"
        elif [[ $1 == */* ]]; then
            echo "$(cd "${1%/*}"; pwd)/${1##*/}"
        else
            echo "$(pwd)/$1"
        fi
    fi
}

function List_functions() {
  grep -E "^function [a-z]" "${BASH_SOURCE[0]}" | sed -e 's/function \([a-z_0-9]*\).*/\1/'
}

if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
  TOOLS_ABSPATH=$(abspath "${BASH_SOURCE[0]}")
  TOOLS_DIR=${TOOLS_DIR:-$(dirname "${TOOLS_ABSPATH}")}
  echo "Script ${BASH_SOURCE[0]} is being sourced (Tools in $TOOLS_DIR)..."
  List_functions
  SOURCED=1
else
  TOOLS_ABSPATH=$(abspath "${0}")
  TOOLS_DIR=${TOOLS_DIR:-$(dirname "${TOOLS_ABSPATH}")}
  echo "$0 is Executed, (Tools in $TOOLS_DIR) (can also be sourced interactively)..."
  echo "In case of errors, retry at the failed step (readyness checks missing)"
  set -e
  SOURCED=0
  if [[ -z "${PROJECT}" ]]; then
    Usage
  fi
fi

function update_gcp_opts() {
  export GCP_OPTS="--project=${PROJECT} --zone=${ZONE}"
}

function Execute() {
  echo "### Running:" "$@" 1>&2
  "$@"
}

function ExecuteEval() {
  echo "### Running:" "$@" 1>&2
  eval "$@"
}


function create_cluster() {
  Execute gcloud container clusters create "$CLUSTER_NAME" "$GCP_OPTS" --machine-type="$MACHINE_TYPE" --num-nodes="$NUM_NODES" --no-enable-legacy-authorization
}

function delete_cluster() {
  echo "Deleting CLUSTER_NAME=$CLUSTER_NAME"
  Execute gcloud container clusters delete "$CLUSTER_NAME" "$GCP_OPTS" -q
}

function create_vm() {
  echo "Obtaining latest ubuntu xenial image name... (takes a few seconds)..."
  VM_IMAGE=${VM_IMAGE:-$(gcloud compute images list --standard-images --filter=name~ubuntu-1604-xenial --limit=1 --uri)}
  echo "Creating VM_NAME=$VM_NAME using VM_IMAGE=$VM_IMAGE"
  Execute gcloud compute instances create "$VM_NAME" "$GCP_OPTS" --machine-type "$MACHINE_TYPE" --image "$VM_IMAGE"
  echo "Waiting a bit for the VM to come up..."
  #TODO: 'wait for vm to be ready'
  sleep 45
}

function delete_vm() {
  echo "Deleting VM_NAME=$VM_NAME"
  Execute gcloud compute instances delete "$VM_NAME" "$GCP_OPTS" -q
}

function run_on_vm() {
  echo "*** Remote run: \"$1\"" 1>&2
  Execute gcloud compute ssh "$VM_NAME" "$GCP_OPTS" --command "$1"
}

function setup_vm() {
  Execute gcloud compute instances add-tags "$VM_NAME" "$GCP_OPTS" --tags https-server
  # shellcheck disable=SC2016
  run_on_vm '(sudo add-apt-repository ppa:gophers/archive > /dev/null && sudo apt-get update > /dev/null && sudo apt-get upgrade --no-install-recommends -y && sudo apt-get install --no-install-recommends -y golang-1.10-go make && mv .bashrc .bashrc.orig && (echo "export PATH=/usr/lib/go-1.10/bin:\$PATH:~/go/bin"; cat .bashrc.orig) > ~/.bashrc ) < /dev/null'
}

function setup_vm_firewall() {
  Execute gcloud compute --project="$PROJECT" firewall-rules create default-allow-https --network=default --action=ALLOW --rules=tcp:443 --source-ranges=0.0.0.0/0 --target-tags=https-server || true
}

function delete_vm_firewall() {
  Execute gcloud compute --project="$PROJECT" firewall-rules delete default-allow-https -q
}

function update_fortio_on_vm() {
  # shellcheck disable=SC2016
  run_on_vm 'go get fortio.org/fortio && cd go/src/fortio.org/fortio && git fetch --tags && git checkout latest_release && make submodule-sync && make official-build-version OFFICIAL_BIN=~/go/bin/fortio && sudo setcap 'cap_net_bind_service=+ep' `which fortio` && fortio version'
}

function run_fortio_on_vm() {
  run_on_vm 'pkill fortio; nohup fortio server -http-port 443 > ~/fortio.log 2>&1 &'
}

function get_vm_ip() {
  VM_IP=$(gcloud compute instances describe "$VM_NAME" "$GCP_OPTS" |grep natIP|awk -F": " '{print $2}')
  VM_URL="http://$VM_IP:443/fortio/"
  echo "+++ VM Ip is $VM_IP - visit (http on port 443 is not a typo:) $VM_URL"
}

# assumes run from istio/ (or release) directory
function install_istio() {
  # You need these permissions to create the necessary RBAC rules for Istio
  # shellcheck disable=SC2016
  Execute sh -c 'kubectl create clusterrolebinding cluster-admin-binding --clusterrole=cluster-admin --user="$(gcloud config get-value core/account)"'
  # Use the non debug ingress and remove the -v "2"
  Execute sh -c 'sed -e "s/_debug//g" install/kubernetes/istio-auth.yaml | egrep -v -e "- (-v|\"2\")" | kubectl apply -f -'
}

# assumes run from istio/ (or release) directory
function delete_istio() {
  # Use the non debug ingress and remove the -v "2"
  Execute sh -c 'kubectl delete -f install/kubernetes/istio-auth.yaml'
}

function kubectl_setup() {
  Execute gcloud container clusters get-credentials "$CLUSTER_NAME" "$GCP_OPTS"
}

function install_non_istio_svc() {
 Execute kubectl create namespace "$FORTIO_NAMESPACE"
 Execute kubectl -n "$FORTIO_NAMESPACE" run fortio1 --image=fortio/fortio:latest_release --port=8080
 Execute kubectl -n "$FORTIO_NAMESPACE" expose deployment fortio1 --target-port=8080 --type=LoadBalancer
 Execute kubectl -n "$FORTIO_NAMESPACE" run fortio2 --image=fortio/fortio:latest_release --port=8080
 Execute kubectl -n "$FORTIO_NAMESPACE" expose deployment fortio2 --target-port=8080
}

function install_istio_svc() {
 Execute kubectl create namespace "$ISTIO_NAMESPACE" || echo "Error assumed to be ns $ISTIO_NAMESPACE already created"
 FNAME=$TOOLS_DIR/perf_k8svcs
 Execute sh -c "$ISTIOCTL kube-inject --debug=$DEBUG -n $ISTIO_NAMESPACE -f $FNAME.yaml > ${FNAME}_istio.yaml"
 Execute kubectl apply -n "$ISTIO_NAMESPACE" -f "${FNAME}_istio.yaml"
}

function install_istio_ingress_rules() {
  # perf istio rules installs rules for both fortio and grafana
  FNAME=$TOOLS_DIR/perf_istio_rules.yaml
  Execute kubectl create -n "$ISTIO_NAMESPACE" -f "$FNAME"
}

function install_istio_cache_busting_rule() {
  FNAME=$TOOLS_DIR/cache_buster.yaml
  Execute kubectl create -n "$ISTIO_NAMESPACE" -f "$FNAME"
}

function get_fortio_k8s_ip() {
  FORTIO_K8S_IP=$(kubectl -n "$FORTIO_NAMESPACE" get svc -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
  while [[ -z "${FORTIO_K8S_IP}" ]]
  do
    echo sleeping to get FORTIO_K8S_IP "$FORTIO_K8S_IP"
    sleep 5
    FORTIO_K8S_IP=$(kubectl -n "$FORTIO_NAMESPACE" get svc -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
  done
  echo "+++ In k8s fortio external ip: http://$FORTIO_K8S_IP:8080/fortio/"
}

# Doesn't work somehow...
function setup_non_istio_ingress2() {
  cat <<_EOF_ | kubectl apply -n fortio -f -
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: fortio-ingress2
spec:
  rules:
  - http:
      paths:
       - path: /fortio1
         backend:
           serviceName: fortio1
           servicePort: 8080
       - path: /fortio2
         backend:
           serviceName: fortio2
           servicePort: 8080
_EOF_
}

function setup_non_istio_ingress() {
  cat <<_EOF_ | kubectl apply -n fortio -f -
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: fortio-ingress
spec:
  backend:
    serviceName: fortio1
    servicePort: 8080
_EOF_
}


function get_non_istio_ingress_ip() {
  K8S_INGRESS_IP=$(kubectl -n "$FORTIO_NAMESPACE" get ingress -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
  while [[ -z "${K8S_INGRESS_IP}" ]]
  do
    echo sleeping to get K8S_INGRESS_IP "${K8S_INGRESS_IP}"
    sleep 5
    K8S_INGRESS_IP=$(kubectl -n "$FORTIO_NAMESPACE" get ingress -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
  done

#  echo "+++ In k8s non istio ingress: http://$K8S_INGRESS_IP/fortio1/fortio/ and fortio2"
  echo "+++ In k8s non istio ingress: http://$K8S_INGRESS_IP/fortio/"
}

function get_istio_ingressgateway_ip() {
  ISTIO_INGRESSGATEWAY_IP=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
  ISTIO_INGRESSGATEWAY_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.name=="http")].port}')
  while [[ -z "${ISTIO_INGRESSGATEWAY_IP}" ]]
  do
    echo sleeping to get ISTIO_INGRESSGATEWAY_IP "${ISTIO_INGRESSGATEWAY_IP}"
    sleep 5
    ISTIO_INGRESSGATEWAY_IP=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
  done

  echo "+++ In k8s istio ingress: http://$ISTIO_INGRESSGATEWAY_IP:$ISTIO_INGRESSGATEWAY_PORT/fortio1/fortio/ and fortio2"
  echo "+++ In k8s grafana: http://$ISTIO_INGRESSGATEWAY_IP:$ISTIO_INGRESSGATEWAY_PORT/d/1/"
}

# Set default QPS to max qps
if [ -z "${QPS+x}" ] || [ "$QPS" == "" ]; then
  echo "Setting default qps"
  QPS=-1
fi

# Set default run duration to 30s
if [ -z "${DUR+x}" ] || [ "$DUR" == "" ]; then
  DUR="30s"
fi

function get_istio_version() {
  kubectl describe pods -n istio|grep /proxyv2:|head -1 | awk -F: '{print $3}'
}

function get_json_file_name() {
  BASE="${1}"
  if [[ $TS == "" ]]; then
    TS=$(date +'%Y-%m-%d-%H-%M')
  fi
  if [[ $VERSION == "" ]]; then
    VERSION=$(get_istio_version)
  fi
  QPSSTR="qps_${QPS}"
  if [[ $QPSSTR == "qps_-1" ]]; then
    QPSSTR="qps_max"
  fi
  LABELS="$BASE $QPSSTR $VERSION"
  FNAME=$QPSSTR-$BASE-$VERSION-$TS
  file_escape
  label_escape
  echo "$FNAME"
}

function file_escape() {
  FNAME=${FNAME// /_}
}

function label_escape() {
  LABELS=${LABELS// /+}
}

function run_fortio_test1() {
  echo "Using default loadbalancer, no istio:"
  Execute curl "$VM_URL?json=on&save=on&qps=$QPS&t=$DUR&c=48&load=Start&url=http://$FORTIO_K8S_IP:8080/echo"
}
function run_fortio_test2() {
  echo "Using default ingress, no istio:"
  Execute curl "$VM_URL?json=on&save=on&qps=$QPS&t=$DUR&c=48&load=Start&url=http://$K8S_INGRESS_IP/echo"
}

function run_fortio_test_istio_ingress1() {
  get_json_file_name "ingress to s1"
  echo "Using istio ingress to fortio1, saving to $FNAME"
  ExecuteEval curl -s "$VM_URL?labels=$LABELS\\&json=on\\&save=on\\&qps=$QPS\\&t=$DUR\\&c=48\\&load=Start\\&url=http://$ISTIO_INGRESSGATEWAY_IP:$ISTIO_INGRESSGATEWAY_PORT/fortio1/echo" \| tee "$FNAME.json" \| grep ActualQPS
}
function run_fortio_test_istio_ingress2() {
  get_json_file_name "ingress to s2"
  echo "Using istio ingress to fortio2, saving to $FNAME"
  ExecuteEval curl -s "$VM_URL?labels=$LABELS\\&json=on\\&save=on\\&qps=$QPS\\&t=$DUR\\&c=48\\&load=Start\\&url=http://$ISTIO_INGRESSGATEWAY_IP:$ISTIO_INGRESSGATEWAY_PORT/fortio2/echo" \| tee "$FNAME.json" \| grep ActualQPS
}
function run_fortio_test_istio_1_2() {
  get_json_file_name "s1 to s2"
  echo "Using istio f1 to f2, saving to $FNAME"
  ExecuteEval curl -s "http://$ISTIO_INGRESSGATEWAY_IP:$ISTIO_INGRESSGATEWAY_PORT/fortio1/fortio/?labels=$LABELS\\&json=on\\&save=on\\&qps=$QPS\\&t=$DUR\\&c=48\\&load=Start\\&url=http://echosrv2:8080/echo" \| tee "$FNAME.json" \| grep ActualQPS
}
function run_fortio_test_istio_2_1() {
  get_json_file_name "s2 to s1"
  echo "Using istio f2 to f1, saving to $FNAME"
  ExecuteEval curl -s "http://$ISTIO_INGRESSGATEWAY_IP:$ISTIO_INGRESSGATEWAY_PORT/fortio2/fortio/?labels=$LABELS\\&json=on\\&save=on\\&qps=$QPS\\&t=$DUR\\&c=48\\&load=Start\\&url=http://echosrv1:8080/echo" \| tee "$FNAME.json" \| grep ActualQPS
}

# Run canonical perf tests.
# The following parameters can be supplied:
# 1) Label:
#    A custom label to use. This is useful when running the same suite against two target binaries/configs.
#    Defaults to "canonical"
# 2) Driver:
#    The load driver to use. Currently "fortio1" and "fortio2" are supported. Defaults to "fortio1".
# 3) Target:
#     The target service for the load. Currently "echo1" and "echo2" are supported.
#     Defaults to "echo2"
# 4) QPS:
#     The QPS to apply. Defaults to 400.
# 5) Duration:
#     The duration of the test. Default is 5 minutes.
# 6) Clients:
#     The number of clients to use. Defaults is 16.
# 7) Outdir:
#     The output dir for collecting the Json results. If not specified, a temporary dir will be created.
function run_canonical_perf_test() {
    LABEL="${1}"
    DRIVER="${2}"
    TARGET="${3}"
    QPS="${4}"
    DURATION="${5}"
    CLIENTS="${6}"
    OUT_DIR="${7}"

    # Set defaults
    LABEL="${LABEL:-canonical}"
    DRIVER="${DRIVER:-fortio1}"
    TARGET="${TARGET:-echo2}"
    QPS="${QPS:-400}"
    DURATION="${DURATION:-5m}"
    CLIENTS="${CLIENTS:-16}"

    get_istio_ingressgateway_ip

    FORTIO1_URL="http://${ISTIO_INGRESSGATEWAY_IP}:${ISTIO_INGRESSGATEWAY_PORT}/fortio1/fortio/"
    FORTIO2_URL="http://${ISTIO_INGRESSGATEWAY_IP}:${ISTIO_INGRESSGATEWAY_PORT}/fortio2/fortio/"
    case "${DRIVER}" in
        "fortio1")
            DRIVER_URL="${FORTIO1_URL}"
            ;;
        "fortio2")
            DRIVER_URL="${FORTIO2_URL}"
            ;;
        *)
            echo "unknown driver: ${DRIVER}" >&2
            exit 1
            ;;
    esac

    # URL encoded URLs for echo1 and echo2. These get directly embedded as parameters into the main URL to invoke
    # the test.
    ECHO1_URL="echosrv1:8080/echo"
    ECHO2_URL="echosrv2:8080/echo"
    case "${TARGET}" in
        "echo1")
            TARGET_URL="${ECHO1_URL}"
            ;;
        "echo2")
            TARGET_URL="${ECHO2_URL}"
            ;;
        *)
            echo "unknown target: ${TARGET}" >&2
            exit 1
            ;;
    esac

    GRANULARITY="0.001"

    LABELS="${LABEL}+${DRIVER}+${TARGET}+Q${QPS}+T${DURATION}+C${CLIENTS}"

    if [[ -z "${OUT_DIR// }" ]]; then
        OUT_DIR=$(mktemp -d -t "istio_perf.XXXXXX")
    fi

    FILE_NAME="${LABELS//\+/_}"
    OUT_FILE="${OUT_DIR}/${FILE_NAME}.json"

    echo "Running '${LABELS}' and storing results in ${OUT_FILE}"

    URL="${DRIVER_URL}/?labels=${LABELS}&url=${TARGET_URL}&qps=${QPS}&t=${DURATION}&c=${CLIENTS}&r=${GRANULARITY}&json=on&save=on&load=Start"
    #echo "URL: ${URL}"

    curl -s "${URL}" -o "${OUT_FILE}"
}

function wait_istio_up() {
  for namespace in $(kubectl get namespaces --no-headers -o name); do
    for name in $(kubectl get deployment -o name -n "${namespace}"); do
      kubectl -n "${namespace}" rollout status "${name}" -w;
    done
  done
}

function setup_vm_all() {
  update_gcp_opts
  create_vm
  setup_vm
  setup_vm_firewall
  update_fortio_on_vm
  run_fortio_on_vm
}

function setup_istio_all() {
  update_gcp_opts
  install_istio
  install_istio_svc
  wait_istio_up #wait
  install_istio_ingress_rules
  install_istio_cache_busting_rule
  wait_istio_up #wait
}

function setup_cluster_all() {
  echo "Setting up CLUSTER_NAME=$CLUSTER_NAME for PROJECT=$PROJECT in ZONE=$ZONE, NUM_NODES=$NUM_NODES * MACHINE_TYPE=$MACHINE_TYPE"
  create_cluster
  kubectl_setup
  install_non_istio_svc
  setup_non_istio_ingress
  setup_istio_all
}

function setup_all() {
  setup_vm_all
  setup_cluster_all
}

function delete_all() {
  echo "Deleting Istio mesh, cluster $CLUSTER_NAME, Instance $VM_NAME and firewall rules for project $PROJECT in zone $ZONE"
  echo "Interrupt now if you don't want to delete..."
  sleep 5
  delete_istio
  delete_cluster
  delete_vm
  delete_vm_firewall
}

function get_ips() {
  #TODO: wait for ingresses/svcs to be ready
  get_vm_ip
  get_fortio_k8s_ip
  get_non_istio_ingress_ip
  get_istio_ingressgateway_ip
}

function run_4_tests() {
  run_fortio_test_istio_ingress1
  run_fortio_test_istio_ingress2
  run_fortio_test_istio_1_2
  run_fortio_test_istio_2_1
}

function run_tests() {
  update_gcp_opts
  get_ips
  VERSION="" # reset in case it changed
  TS="" # reset once per set
  QPS=-1
  run_4_tests
  QPS=400
  TS="" # reset once per set
  run_4_tests
  echo "Graph the results:"
  fortio report &
}


function check_image_versions() {
  kubectl get pods --all-namespaces -o jsonpath="{..image}" | tr -s '[:space:]' '\n' | sort | uniq -c | grep -v -e google.containers
}

if [[ $SOURCED == 0 ]]; then
  # Normal mode: all at once:
  update_gcp_opts
  setup_all

#update_fortio_on_vm
#run_fortio_on_vm
#setup_vm_all

# test/retry one step at a time, eg.
#install_non_istio_svc
#setup_non_istio_ingress
#get_non_istio_ingress_ip
#setup_istio_all
#install_istio_svc
#install_istio_ingress_rules
#setup_non_istio_ingress
#install_istio
#setup_vm_firewall
#get_ips
  run_tests
#setup_vm_firewall
#get_ips
#install_istio_svc
#delete_all
fi
