#!/usr/bin/env bash

# Helper functions for extending the cluster with external VMs.

# Script can be sourced in other files or used from tools like ansible.
# Environment variables used:

# ISTIO_NAMESPACE - if not set the default in .kube/config is used.
# ISTIO_FILES - directory where istio artifacts are downloaded, default to current dir

# Initialize internal load balancers to access K8S DNS and Istio Pilot, Mixer, CA.
# Must be run once per cluster.
function istioInitILB() {
   local NS=""
   if [[ ${ISTIO_NAMESPACE:-} != "" ]]; then
     NS="-n $ISTIO_NAMESPACE"
   fi

cat <<EOF | kubectl apply $NS -f -
apiVersion: v1
kind: Service
metadata:
  name: istio-pilot-ilb
  annotations:
    cloud.google.com/load-balancer-type: "internal"
  labels:
    istio: pilot
spec:
  type: LoadBalancer
  ports:
  - port: 8080
    protocol: TCP
  selector:
    istio: pilot
EOF
cat <<EOF | kubectl apply -n kube-system -f -
apiVersion: v1
kind: Service
metadata:
  name: dns-ilb
  annotations:
    cloud.google.com/load-balancer-type: "internal"
  labels:
    k8s-app: kube-dns
spec:
  type: LoadBalancer
  ports:
  - port: 53
    protocol: UDP
  selector:
    k8s-app: kube-dns
EOF

cat <<EOF | kubectl apply  $NS -f -
apiVersion: v1
kind: Service
metadata:
  name: mixer-ilb
  annotations:
    cloud.google.com/load-balancer-type: "internal"
  labels:
    istio: mixer
spec:
  type: LoadBalancer
  ports:
  - port: 9091
    protocol: TCP
  selector:
    istio: mixer
EOF

cat <<EOF | kubectl apply $NS -f -
apiVersion: v1
kind: Service
metadata:
  name: istio-ca-ilb
  annotations:
    cloud.google.com/load-balancer-type: "internal"
  labels:
    istio: istio-ca
spec:
  type: LoadBalancer
  ports:
  - port: 8060
    protocol: TCP
  selector:
    istio: istio-ca
EOF
}

# Generate a kubedns file and a cluster.env
# Parameters:
# - name of the k8s cluster.
function istioGenerateClusterConfigs() {
   local K8SCLUSTER=${1}

   local NS=""
   if [[ ${ISTIO_NAMESPACE:-} != "" ]]; then
     NS="-n $ISTIO_NAMESPACE"
   fi

  # Multiple tries, it may take some time until the controllers generate the IPs
  for i in {1..10}
  do
    PILOT_IP=$(kubectl get $NS service istio-pilot-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    ISTIO_DNS=$(kubectl get -n kube-system service dns-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    MIXER_IP=$(kubectl get $NS service mixer-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    CA_IP=$(kubectl get $NS service istio-ca-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

    if [ ${PILOT_IP} == "" -o  ${PILOT_IP} == "" -o ${MIXER_IP} == "" ] ; then
        echo Waiting for ILBs
        sleep 5
    else
        break
    fi
  done

  if [ ${PILOT_IP} == "" -o  ${PILOT_IP} == "" -o ${MIXER_IP} == "" ] ; then
    echo "Failed to create ILBs"
    exit 1
  fi

  #/etc/dnsmasq.d/kubedns
  echo "server=/svc.cluster.local/$ISTIO_DNS" > kubedns
  echo "address=/istio-mixer/$MIXER_IP" >> kubedns
  echo "address=/mixer-server/$MIXER_IP" >> kubedns
  echo "address=/istio-pilot/$PILOT_IP" >> kubedns
  echo "address=/istio-ca/$CA_IP" >> kubedns

  CIDR=$(gcloud container clusters describe ${K8SCLUSTER} $(_istioGcloudOpt) --format "value(servicesIpv4Cidr)")
  echo "ISTIO_SERVICE_CIDR=$CIDR" > cluster.env
}


# Install required files on a VM and run the setup script.
#
# Must be run for each VM added to the cluster
# Params:
# - name of the VM - used to copy files over.
# - service account to be provisioned (defaults to istio.default)
function istioProvisionVM() {
 NAME=${1}

 local SA=${2:-istio.default}

  kubectl get secret $SA -o jsonpath='{.data.cert-chain\.pem}' |base64 -d  > cert-chain.pem
  kubectl get secret $SA -o jsonpath='{.data.root-cert\.pem}' |base64 -d  > root-cert.pem
  kubectl get secret $SA -o jsonpath='{.data.key\.pem}' |base64 -d  > key.pem

  local ISTIO_FILES=${ISTIO_FILES:-.}

 # Copy deb, helper and config files
 # Reviews not copied - VMs don't support labels yet.
 istioCopy $NAME \
   kubedns \
   *.pem \
   cluster.env \
   $ISTIO_FILES/istio_vm_setup.sh \
   $ISTIO_FILES/istio-proxy-envoy.deb \
   $ISTIO_FILES/istio-agent.deb \
   $ISTIO_FILES/istio-auth-node-agent.deb \

 # Run the setup script.
 istioRun $NAME "sudo bash -c -x ./istio_vm_setup.sh"
}


# Helper functions for the main script

# If Istio was built from source, copy the artifcats to the current directory, for use
# by istioProvisionVM
function istioCopyBuildFiles() {
  local ISTIO_IO=${ISTIO_BASE:-${GOPATH:-$HOME/go}}/src/istio.io
  local ISTIO_FILES=${ISTIO_FILES:-.}

  (cd $ISTIO_IO/proxy; bazel build tools/deb/... )
  (cd $ISTIO_IO/pilot; bazel build tools/deb/... )
  (cd $ISTIO_IO/auth; bazel build tools/deb/... )

  cp $ISTIO_IO/proxy/bazel-bin/tools/deb/istio-proxy-envoy.deb \
     $ISTIO_IO/pilot/bazel-bin/tools/deb/istio-agent.deb \
     $ISTIO_IO/auth/bazel-bin/tools/deb/istio-auth-node-agent.deb \
     $ISTIO_IO/istio/install/tools/istio_vm_setup.sh \
     $ISTIO_FILES

}

# Copy files to the VM
function istioCopy() {
  # TODO: based on some env variable, use different commands for other clusters or for testing with
  # bare-metal machines.
  local NAME=$1
  shift
  local FILES=$*

  gcloud compute scp --recurse $(_istioGcloudOpt) $FILES ${NAME}:
}

# Run a command in a VM.
function istioRun() {
  local NAME=$1
  local CMD=$2

  gcloud compute ssh $(_istioGcloudOpt) --command "$CMD"
}

# Helper to generate options for gcloud compute.
# PROJECT and ISTIO_ZONE are optional. If not set, the defaults will be used.
#
# Example default setting:
# gcloud config set compute/zone us-central1-a
# gcloud config set core/project costin-raw
function _istioGcloudOpt() {
    local OPTS=""
    if [[ ${PROJECT:-} != "" ]]; then
       OPTS="--project $PROJECT"
    fi
    if [[ ${ISTIO_ZONE:-} != "" ]]; then
       OPTS="$OPTS --zone $ISTIO_ZONE"
    fi
    echo $OPTS
}
