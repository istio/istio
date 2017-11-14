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
FORTIO_NAMESPACE=${FORTIO_NAMESPACE:-fortio} # Namespace for non istio app
ISTIO_NAMESPACE=${ISTIO_NAMESPACE:-istio} # Namespace for istio injected app
TOOLS_DIR=${TOOLS_DIR:-$(dirname $0)}

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
  echo "Obtaining latest ubuntu xenial image name... (takes a few seconds)..."
  VM_IMAGE=${VM_IMAGE:-$(gcloud compute images list --standard-images --filter=name~ubuntu-1604-xenial --limit=1 --uri)}
  echo "Creating VM_NAME=$VM_NAME using VM_IMAGE=$VM_IMAGE"
  execute gcloud compute instances create $VM_NAME $GCP_OPTS --machine-type $MACHINE_TYPE --image $VM_IMAGE
}

function run_on_vm() {
  echo "*** Remote run: \"$1\""
  execute gcloud compute ssh $VM_NAME $GCP_OPTS --command "$1"
}

function setup_vm() {
  execute gcloud compute instances add-tags $VM_NAME $GCP_OPTS --tags http-server,allow-8080
  run_on_vm '(sudo add-apt-repository ppa:gophers/archive > /dev/null && sudo apt-get update > /dev/null && sudo apt-get upgrade --no-install-recommends -y && sudo apt-get install --no-install-recommends -y golang-1.8-go && mv .bashrc .bashrc.orig && (echo "export PATH=/usr/lib/go-1.8/bin:\$PATH:~/go/bin"; cat .bashrc.orig) > ~/.bashrc ) < /dev/null'
}

function setup_vm_firewall() {
  execute gcloud compute --project=$PROJECT firewall-rules create default-allow-http --network=default --action=ALLOW --rules=tcp:80 --source-ranges=0.0.0.0/0 --target-tags=http-server || true
  execute gcloud compute --project $PROJECT firewall-rules create allow-8080 --direction=INGRESS --action=ALLOW --rules=tcp:8080 --target-tags=port8080 || true
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
  execute sh -c 'sed -e "s/_debug//g" install/kubernetes/istio-auth.yaml | egrep -v -e "- (-v|\"2\")" | kubectl apply -f -'
}

function kubectl_setup() {
  execute gcloud container clusters get-credentials $CLUSTER_NAME $GCP_OPTS
}

function install_non_istio_svc() {
 execute kubectl create namespace $FORTIO_NAMESPACE
 execute kubectl -n $FORTIO_NAMESPACE run fortio --image=istio/fortio --port=8080
 execute kubectl -n $FORTIO_NAMESPACE expose deployment fortio --target-port=8080 --type=LoadBalancer
}

function install_istio_svc() {
 #execute kubectl create namespace $ISTIO_NAMESPACE
 FNAME=$TOOLS_DIR/perf_k8svcs
 execute sh -c "$ISTIOCTL kube-inject --debug=false -n $ISTIO_NAMESPACE -f $FNAME.yaml > ${FNAME}_istio.yaml"
 execute kubectl apply -n $ISTIO_NAMESPACE -f ${FNAME}_istio.yaml
}

function install_istio_ingress_rules() {
  FNAME=$TOOLS_DIR/perf_istio_rules.yaml
  execute $ISTIOCTL create -n $ISTIO_NAMESPACE -f $FNAME
}


function get_fortio_k8s_ip() {
  FORTIO_K8S_IP=$(kubectl -n $FORTIO_NAMESPACE get svc -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
  echo "+++ In k8s fortio external ip: http://$FORTIO_K8S_IP:8080/fortio/"
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
  echo "+++ In k8s non istio ingress: http://$K8S_INGRESS_IP/fortio/"
}

function get_istio_ingress_ip() {
  ISTIO_INGRESS_IP=$(kubectl -n $ISTIO_NAMESPACE get ingress -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
  echo "+++ In k8s istio ingress: http://$ISTIO_INGRESS_IP/fortio1/fortio/ and fortio2"
}

function run_fortio_test1() {
  echo "Using default loadbalancer, no istio:"
  execute curl "http://$VM_IP/fortio/?json=on&qps=-1&t=30s&c=48&load=Start&url=http://$FORTIO_K8S_IP:8080/echo"
}
function run_fortio_test2() {
  echo "Using default ingress, no istio:"
  execute curl "http://$VM_IP/fortio/?json=on&qps=-1&t=30s&c=48&load=Start&url=http://$K8S_INGRESS_IP/echo"
}
function run_fortio_test3() {
  echo "Using istio ingress:"
  execute curl "http://$VM_IP/fortio/?json=on&qps=-1&t=30s&c=48&load=Start&url=http://$ISTIO_INGRESS_IP/fortio1/echo"
}

echo "Setting up CLUSTER_NAME=$CLUSTER_NAME for PROJECT=$PROJECT in ZONE=$ZONE, NUM_NODES=$NUM_NODES * MACHINE_TYPE=$MACHINE_TYPE"

function setup_vm_all() {
  create_vm
  #TODO: 'wait for vm to be ready'
  setup_vm
  setup_vm_firewall
  update_fortio_on_vm
  run_fortio_on_vm
}

function setup_istio_all() {
  install_istio
  install_istio_svc
  install_istio_ingress_rules
}

function setup_cluster_all() {
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
  get_vm_ip
  get_fortio_k8s_ip
  get_non_istio_ingress_ip
  get_istio_ingress_ip
}

function run_tests() {
  setup_vm_firewall
  get_ips
  run_fortio_test1
  run_fortio_test2
  run_fortio_test3
}
# Normal mode: all at once:
setup_all

#update_fortio_on_vm
#run_fortio_on_vm
#setup_vm_all

# test/retry one step at a time, eg.
# setup_non_istio_ingress
#install_non_istio_svc
#setup_istio_all
#install_istio_svc
#install_istio_ingress
#install_istio_ingress_rules
#setup_non_istio_ingress
#install_istio
#setup_vm_firewall
#get_ips

run_tests
