#! /bin/bash
# Sets up a cluster for perf testing - GCP/GKE
# PROJECT=istio-perf tools/setup_perf_cluster.sh
set -e

ZONE=${ZONE:-us-east4-b}
CLUSTER_NAME=${CLUSTER_NAME:-istio-perf}
MACHINE_TYPE=${MACHINE_TYPE:-n1-highcpu-2}
NUM_NODES=${NUM_NODES:-6} # SvcA<->SvcB + Ingress + Pilot + Mixer + 1 extra (kube-system)
VM_NAME=${VM_NAME:-fortio-vm}
ISTIOCTL=${ISTIOCTL:-istioctl} # to override istioctl from outside of the path
FORTIO_NAMESPACE=${FORTIO_NAMESPACE:-fortio}

function usage() {
    echo "usage: PROJECT=project ZONE=zone $0"
    echo "also settable are NUM_NODES, MACHINE_TYPE, CLUSTER_NAME, VM_NAME, VM_IMAGE"
    exit 1
}

if [[ -z "${PROJECT}" ]]; then
  usage
fi

GCP_OPTS="--project $PROJECT --zone $ZONE"

function execute() {
  echo "### Running:" "$@"
  "$@"
}

function create_cluster() {
  execute gcloud container clusters create $CLUSTER_NAME $GCP_OPTS --machine-type=$MACHINE_TYPE --num-nodes=$NUM_NODES --no-enable-legacy-authorization
}

function create_vm() {
  VM_IMAGE=${VM_IMAGE:-$(gcloud compute images list --standard-images --filter=name~ubuntu-1604-xenial --limit=1 --uri)}
  echo "Creating VM_NAME=$VM_NAME using VM_IMAGE=$VM_IMAGE"
  execute gcloud compute instances create $VM_NAME $GCP_OPTS --machine-type $MACHINE_TYPE --image $VM_IMAGE
}

function run_on_vm() {
  echo "*** Remote run: \"$1\""
  execute gcloud compute ssh $VM_NAME $GCP_OPTS --command "$1"
}

function setup_vm() {
  run_on_vm 'sudo add-apt-repository ppa:gophers/archive > /dev/null && sudo apt-get update > /dev/null && sudo apt-get install --no-install-recommends -y golang-1.8-go && mv .bashrc .bashrc.orig && (echo "export PATH=/usr/lib/go-1.8/bin:\$PATH:~/go/bin"; cat .bashrc.orig) > ~/.bashrc'
  execute gcloud compute instances add-tags $VM_NAME $GCP_OPTS --tags port8080
  execute gcloud compute --project $PROJECT firewall-rules create allow-8080 --direction=INGRESS --action=ALLOW --rules=tcp:8080 --target-tags=port8080
}

function update_fortio_on_vm() {
  run_on_vm 'go get -u istio.io/fortio'
}

function run_fortio_on_vm() {
  run_on_vm 'pkill fortio; nohup fortio server > ~/fortio.log 2>&1 &'
}

function get_vm_ip() {
  VM_IP=$(gcloud compute instances describe $VM_NAME $GCP_OPTS |grep natIP|awk -F": " '{print $2}')
  echo "+++ VM Ip is $VM_IP - visit http://$VM_IP:8080/fortio"
}

# assumes run from istio/ (or release) directory
function install_istio() {
  # Use the non debug ingress:
  execute sh -c 'sed -e "s/_debug//g" install/kubernetes/istio-auth.yaml | kubectl apply -f -'
}

function kubectl_setup() {
  execute gcloud container clusters get-credentials $CLUSTER_NAME $GCP_OPTS
}

function install_non_istio_svc() {
 execute kubectl create namespace $FORTIO_NAMESPACE
 execute kubectl -n $FORTIO_NAMESPACE run fortio --image=istio/fortio --port=8080
 execute kubectl -n $FORTIO_NAMESPACE expose deployment fortio --target-port=8080 --type=LoadBalancer
}

function get_fortio_k8s_ip() {
  FORTIO_K8S_IP=$(kubectl -n $FORTIO_NAMESPACE get svc -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
  echo "+++ In k8s fortio external ip: http://$FORTIO_K8S_IP:8080/fortio"
}

function setup_non_istio_ingress() {
  cat <<_EOF_ | kubectl apply -n fortio -f -
apiVersion: extensions/v1beta1
kind: Ingress
metadata:
  name: fortio-ingress
spec:
  backend:
    serviceName: fortio
    servicePort: 8080
_EOF_
}

function get_non_istio_ingress_ip() {
  K8S_INGRESS_IP=$(kubectl -n $FORTIO_NAMESPACE get ingress -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
  echo "+++ In k8s non istio ingress: http://$K8S_INGRESS_IP:8080/fortio"
}

function run_fortio_test1() {
  execute curl -v "http://$VM_IP:8080/fortio?json=on&qps=-1&t=30s&c=48&load=Start&url=http://$FORTIO_K8S_IP:8080/echo"
}
function run_fortio_test2() {
  execute curl -v "http://$VM_IP:8080/fortio?json=on&qps=-1&t=30s&c=48&load=Start&url=http://$K8S_INGRESS_IP:8080/echo"
}

echo "Setting up CLUSTER_NAME=$CLUSTER_NAME for PROJECT=$PROJECT in ZONE=$ZONE, NUM_NODES=$NUM_NODES * MACHINE_TYPE=$MACHINE_TYPE"

function setup_vm_all() {
  create_vm
  setup_vm
  update_fortio_on_vm
  run_fortio_on_vm
}

function setup_cluster_all() {
  create_cluster
  kubectl_setup
  install_non_istio_svc
  setup_non_istio_ingress
  install_istio
}

function setup_all() {
  setup_vm_all
  setup_cluster_all
}

function get_ips() {
  get_vm_ip
  get_fortio_k8s_ip
  get_non_istio_ingress_ip
}

function run_tests() {
  get_ips
  run_fortio_test1
  run_fortio_test2
}
# Normal mode: all at once:
setup_all

# test/retry one step at a time, eg.
# setup_non_istio_ingress
#install_non_istio_svc
run_tests
#setup_non_istio_ingress
