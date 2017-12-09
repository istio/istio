#! /bin/bash
#
# Sets up a cluster for perf testing - GCP/GKE
# PROJECT=istio-perf tools/setup_perf_cluster.sh
#
# This can be used as a script or sourced and functions called interactively
#

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

function List_functions() {
  egrep "^function [a-z]" ${BASH_SOURCE[0]} | sed -e 's/function \([a-z_0-9]*\).*/\1/'
}

if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
  TOOLS_DIR=${TOOLS_DIR:-$(dirname ${BASH_SOURCE[0]})}
  echo "Script ${BASH_SOURCE[0]} is being sourced (Tools in $TOOLS_DIR)..."
  echo "You must set PROJECT before using one of the function interactively:"
  List_functions
  SOURCED=1
else
  TOOLS_DIR=${TOOLS_DIR:-$(dirname $0)}
  echo "$0 is Executed, (Tools in $TOOLS_DIR) (can also be sourced interactively)..."
  echo "In case of errors, retry at the failed step (readyness checks missing)"
  set -e
  SOURCED=0
  if [[ -z "${PROJECT}" ]]; then
    Usage
  fi
fi


function update_gcp_opts() {
  export GCP_OPTS="--project $PROJECT --zone $ZONE"
}

function Execute() {
  echo "### Running:" "$@"
  "$@"
}

function create_cluster() {
  Execute gcloud container clusters create $CLUSTER_NAME $GCP_OPTS --machine-type=$MACHINE_TYPE --num-nodes=$NUM_NODES --no-enable-legacy-authorization
}

function create_vm() {
  echo "Obtaining latest ubuntu xenial image name... (takes a few seconds)..."
  VM_IMAGE=${VM_IMAGE:-$(gcloud compute images list --standard-images --filter=name~ubuntu-1604-xenial --limit=1 --uri)}
  echo "Creating VM_NAME=$VM_NAME using VM_IMAGE=$VM_IMAGE"
  Execute gcloud compute instances create $VM_NAME $GCP_OPTS --machine-type $MACHINE_TYPE --image $VM_IMAGE
}

function run_on_vm() {
  echo "*** Remote run: \"$1\""
  Execute gcloud compute ssh $VM_NAME $GCP_OPTS --command "$1"
}

function setup_vm() {
  Execute gcloud compute instances add-tags $VM_NAME $GCP_OPTS --tags http-server,allow-8080
  run_on_vm '(sudo add-apt-repository ppa:gophers/archive > /dev/null && sudo apt-get update > /dev/null && sudo apt-get upgrade --no-install-recommends -y && sudo apt-get install --no-install-recommends -y golang-1.8-go && mv .bashrc .bashrc.orig && (echo "export PATH=/usr/lib/go-1.8/bin:\$PATH:~/go/bin"; cat .bashrc.orig) > ~/.bashrc ) < /dev/null'
}

function setup_vm_firewall() {
  Execute gcloud compute --project=$PROJECT firewall-rules create default-allow-http --network=default --action=ALLOW --rules=tcp:80 --source-ranges=0.0.0.0/0 --target-tags=http-server || true
  Execute gcloud compute --project $PROJECT firewall-rules create allow-8080 --direction=INGRESS --action=ALLOW --rules=tcp:8080 --target-tags=port8080 || true
}

function update_fortio_on_vm() {
  run_on_vm 'go get -u istio.io/fortio && sudo setcap 'cap_net_bind_service=+ep' `which fortio`'
}

function run_fortio_on_vm() {
  run_on_vm 'pkill fortio; nohup fortio server -http-port 80 > ~/fortio.log 2>&1 &'
}

function get_vm_ip() {
  VM_IP=$(gcloud compute instances describe $VM_NAME $GCP_OPTS |grep natIP|awk -F": " '{print $2}')
  echo "+++ VM Ip is $VM_IP - visit http://$VM_IP/fortio/"
}

# assumes run from istio/ (or release) directory
function install_istio() {
  # Use the non debug ingress and remove the -v "2"
  Execute sh -c 'sed -e "s/_debug//g" install/kubernetes/istio-auth.yaml | egrep -v -e "- (-v|\"2\")" | kubectl apply -f -'
}

function kubectl_setup() {
  Execute gcloud container clusters get-credentials $CLUSTER_NAME $GCP_OPTS
}

function install_non_istio_svc() {
 Execute kubectl create namespace $FORTIO_NAMESPACE
 Execute kubectl -n $FORTIO_NAMESPACE run fortio1 --image=istio/fortio --port=8080
 Execute kubectl -n $FORTIO_NAMESPACE expose deployment fortio1 --target-port=8080 --type=LoadBalancer
 Execute kubectl -n $FORTIO_NAMESPACE run fortio2 --image=istio/fortio --port=8080
 Execute kubectl -n $FORTIO_NAMESPACE expose deployment fortio2 --target-port=8080
}

function install_istio_svc() {
 Execute kubectl create namespace $ISTIO_NAMESPACE || echo "Error assumed to be $ISTIO_NAMESPACE already created"
 FNAME=$TOOLS_DIR/perf_k8svcs
 Execute sh -c "$ISTIOCTL kube-inject --debug=$DEBUG -n $ISTIO_NAMESPACE -f $FNAME.yaml > ${FNAME}_istio.yaml"
 Execute kubectl apply -n $ISTIO_NAMESPACE -f ${FNAME}_istio.yaml
}

function install_istio_ingress_rules() {
  FNAME=$TOOLS_DIR/perf_istio_rules.yaml
  Execute $ISTIOCTL create -n $ISTIO_NAMESPACE -f $FNAME
}


function get_fortio_k8s_ip() {
  FORTIO_K8S_IP=$(kubectl -n $FORTIO_NAMESPACE get svc -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
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
  K8S_INGRESS_IP=$(kubectl -n $FORTIO_NAMESPACE get ingress -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
#  echo "+++ In k8s non istio ingress: http://$K8S_INGRESS_IP/fortio1/fortio/ and fortio2"
  echo "+++ In k8s non istio ingress: http://$K8S_INGRESS_IP/fortio/"
}

function get_istio_ingress_ip() {
  ISTIO_INGRESS_IP=$(kubectl -n $ISTIO_NAMESPACE get ingress -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
  echo "+++ In k8s istio ingress: http://$ISTIO_INGRESS_IP/fortio1/fortio/ and fortio2"
}

function run_fortio_test1() {
  echo "Using default loadbalancer, no istio:"
  Execute curl "http://$VM_IP/fortio/?json=on&qps=-1&t=30s&c=48&load=Start&url=http://$FORTIO_K8S_IP:8080/echo"
}
function run_fortio_test2() {
  echo "Using default ingress, no istio:"
  Execute curl "http://$VM_IP/fortio/?json=on&qps=-1&t=30s&c=48&load=Start&url=http://$K8S_INGRESS_IP/echo"
}
function run_fortio_test3() {
  echo "Using istio ingress:"
  Execute curl "http://$VM_IP/fortio/?json=on&qps=-1&t=30s&c=48&load=Start&url=http://$ISTIO_INGRESS_IP/fortio1/echo"
}

function setup_vm_all() {
  update_gcp_opts
  create_vm
  #TODO: 'wait for vm to be ready'
  sleep 10
  setup_vm
  setup_vm_firewall
  update_fortio_on_vm
  run_fortio_on_vm
}

function setup_istio_all() {
  update_gcp_opts
  install_istio
  install_istio_svc
  install_istio_ingress_rules
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

function get_ips() {
  #TODO: wait for ingresses/svcs to be ready
  get_vm_ip
  get_fortio_k8s_ip
  get_non_istio_ingress_ip
  get_istio_ingress_ip
}

function run_tests() {
  update_gcp_opts
  setup_vm_firewall
  get_ips
  run_fortio_test1
  run_fortio_test2
  run_fortio_test3
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
#install_istio_ingress
#install_istio_ingress_rules
#setup_non_istio_ingress
#install_istio
#setup_vm_firewall
#get_ips
  run_tests
#setup_vm_firewall
#get_ips
#install_istio_svc
fi
