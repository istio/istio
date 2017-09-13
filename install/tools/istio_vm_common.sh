#!/usr/bin/env bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
################################################################################


# Helper functions for extending the mesh with external VMs.

# Script can be sourced in other files or used from tools like ansible.
# Currently the script include helpers for GKE, other providers will be added as we
# test them.

# Environment variables used:
#
# ISTIO_FILES - directory where istio artifacts are downloaded, default to current dir
# ISTIO_NAMESPACE - defaults to istio-system, needs to be set for custom deployments
# K8S_CLUSTER - name of the K8S cluster.
# SERVICE_ACCOUNT - what account to provision on the VM. Defaults to istio.default.
# SERVICE_NAMESPACE-  namespace where the service account and service are running. Defaults to
#  the current workspace in kube config.

# GCLOUD_OPTS - optional parameters for gcloud command, for example "--project P --zone Z".
#  If not set, defaults are used.
# ISTIO_CP - command to use to copy files to the VM.
# ISTIO_RUN - command to use to copy files to the VM.


# Initialize internal load balancers to access K8S DNS and Istio Pilot, Mixer, CA.
# Must be run once per cluster.
function istioInitILB() {
   local NS=${ISTIO_NAMESPACE:-istio-system}

cat <<EOF | kubectl apply -n $NS -f -
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

cat <<EOF | kubectl apply -n $NS -f -
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

cat <<EOF | kubectl apply -n $NS -f -
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
   local K8SCLUSTER=${1:-K8SCLUSTER}

   local NS=${ISTIO_NAMESPACE:-istio-system}

  # Multiple tries, it may take some time until the controllers generate the IPs
  for i in {1..10}
  do
    PILOT_IP=$(kubectl get -n $NS service istio-pilot-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    ISTIO_DNS=$(kubectl get -n kube-system service dns-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    MIXER_IP=$(kubectl get -n $NS service mixer-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    CA_IP=$(kubectl get -n $NS service istio-ca-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

    if [ "${PILOT_IP}" == "" -o  "${ISTIO_DNS}" == "" -o "${MIXER_IP}" == "" ] ; then
        echo Waiting for ILBs
        sleep 5
    else
        break
    fi
  done

  if [ "${PILOT_IP}" == "" -o  "${ISTIO_DNS}" == "" -o "${MIXER_IP}" == "" ] ; then
    echo "Failed to create ILBs"
    exit 1
  fi

  #/etc/dnsmasq.d/kubedns
  echo "server=/svc.cluster.local/$ISTIO_DNS" > kubedns
  echo "address=/istio-mixer/$MIXER_IP" >> kubedns
  echo "address=/mixer-server/$MIXER_IP" >> kubedns
  echo "address=/istio-pilot/$PILOT_IP" >> kubedns
  echo "address=/istio-ca/$CA_IP" >> kubedns

  CIDR=$(gcloud container clusters describe ${K8SCLUSTER} ${GCP_OPTS:-} --format "value(servicesIpv4Cidr)")
  echo "ISTIO_SERVICE_CIDR=$CIDR" > cluster.env
}

# Get an istio service account secret, extract it to files to be provisioned on a raw VM
# Params:
# - service account -  defaults to istio.default or SERVICE_ACCOUNT env
# - service namespace - defaults to current namespace.
function istio_provision_certs() {
  local SA=${1:-${SERVICE_ACCOUNT:-istio.default}}
  local NS=${2:-${SERVICE_NAMESPACE:-}}

  if [[ -n "$NS" ]] ; then
    NS="-n $NS"
  fi

  kubectl get $NS secret $SA -o jsonpath='{.data.cert-chain\.pem}' |base64 -d  > cert-chain.pem
  kubectl get $NS secret $SA -o jsonpath='{.data.root-cert\.pem}' |base64 -d  > root-cert.pem
  kubectl get $NS secret $SA -o jsonpath='{.data.key\.pem}' |base64 -d  > key.pem
}


# Install required files on a VM and run the setup script.
#
# Must be run for each VM added to the cluster
# Params:
# - name of the VM - used to copy files over.
# - optional service account to be provisioned (defaults to istio.default)
# - optional namespace of the service account and VM services, defaults to SERVICE_NAMESPACE env
# or kube config.
function istioBootstrapVM() {
 local NAME=${1}

 local SA=${2:-${SERVICE_ACCOUNT:-istio.default}}
 local NS=${3:-${SERVICE_NAMESPACE:-}}

  istio_provision_certs $SA

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
  # For override to work
  chmod +w *.deb
}

# Copy files to the VM.
# - VM name - required, destination where files will be copied
# - list of files and directories to be copied
function istioCopy() {
  # TODO: based on some env variable, use different commands for other clusters or for testing with
  # bare-metal machines.
  local NAME=$1
  shift
  local FILES=$*

  ${ISTIO_CP:-gcloud compute scp --recurse ${GCP_OPTS:-}} $FILES ${NAME}:
}

# Run a command in a VM.
# - VM name
# - command to run, as one parameter.
function istioRun() {
  local NAME=$1
  local CMD=$2

  ${ISTIO_RUN:-gcloud compute ssh ${GCP_OPTS:-}} $NAME --command "$CMD"
}

