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
# Currently the script include helpers for GKE, other providers will be added as
# they are contributed and we test them.

# Environment variables used:
#
# ISTIO_NAMESPACE - control plane namespace, defaults to istio-system, only
# needs to be set for custom deployments
# K8S_CLUSTER - name of the K8S cluster.
# SERVICE_ACCOUNT - what account to provision on the VM. Defaults to default.
# SERVICE_NAMESPACE-  namespace where the service account and service are
# running. Defaults to the current workspace in kube config.
# ISTIO_SECRET_PREFIX - prefix where the istio CA generates secrets for each
# service account. defaults to "istio."

# GCP_OPTS - optional parameters for gcloud command, for example
# "--project P --zone Z".
# If not set, defaults are used.
# ISTIO_CP - command to use to copy files to the VM.
# ISTIO_RUN - command to use to run a command on the VM.

# Generate a 'kubedns' Dnsmasq config file using the internal load balancer.
# It will need to be installed on each machine expanding the mesh.
function istioDnsmasq() {
   local NS=${ISTIO_NAMESPACE:-istio-system}

  # Multiple tries, it may take some time until the controllers generate the IPs
  for i in {1..10}
  do
    PILOT_IP=$(kubectl get -n $NS service istio-pilot-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    ISTIO_DNS=$(kubectl get -n kube-system service dns-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    MIXER_IP=$(kubectl get -n $NS service mixer-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    CA_IP=$(kubectl get -n $NS service istio-ca-ilb -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

    if [ "${PILOT_IP}" == "" -o  "${ISTIO_DNS}" == "" -o "${MIXER_IP}" == "" ] ; then
        echo "Waiting for ILBs, pilot=$PILOT_IP, MIXER_IP=$MIXER_IP, CA_IP=$CA_IP, DNS=$ISTIO_DNS - kubectl get -n $NS service: $(kubectl get -n $NS service)"
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
  # Also generate host entries for the istio-system. The generated config will work with both
  # 'cluster-wide' and 'per-namespace'.
  echo "address=/istio-mixer.$NS/$MIXER_IP" >> kubedns
  echo "address=/mixer-server.$NS/$MIXER_IP" >> kubedns
  echo "address=/istio-pilot.$NS/$PILOT_IP" >> kubedns
  echo "address=/istio-ca.$NS/$CA_IP" >> kubedns

  echo "Generated Dnsmaq config file 'kubedns'. Install it in /etc/dnsmasq.d and restart dnsmasq."
}

# Generate a cluster.env config file.
# Parameters:
# - name of the k8s cluster.
function istioClusterEnv() {
   local K8S_CLUSTER=${1:-${K8S_CLUSTER}}

   # TODO: parse it all from $(kubectl config current-context)
   CIDR=$(gcloud container clusters describe ${K8S_CLUSTER} ${GCP_OPTS:-} --format "value(servicesIpv4Cidr)")
   echo "ISTIO_SERVICE_CIDR=$CIDR" > cluster.env

  echo "Generated cluster.env, needs to be installed in each VM as /var/lib/istio/envoy/cluster.env"
  echo "The /var/lib/istio/envoy/ directory and files must be readable by 'istio-proxy' user"
}


# Get an istio service account secret, extract it to files to be provisioned on a raw VM
# Params:
# - service account -  defaults to istio.default or SERVICE_ACCOUNT env
# - service namespace - defaults to current namespace.
function istio_provision_certs() {
  local SA=${1:-${SERVICE_ACCOUNT:-default}}
  local NS=${2:-${SERVICE_NAMESPACE:-}}
  local CERT_NAME=${ISTIO_SECRET_PREFIX:-istio.}${SA}

  if [[ -n "$NS" ]] ; then
    NS="-n $NS"
  fi

  kubectl get $NS secret $CERT_NAME -o jsonpath='{.data.cert-chain\.pem}' |base64 -d  > cert-chain.pem
  kubectl get $NS secret $CERT_NAME -o jsonpath='{.data.root-cert\.pem}' |base64 -d  > root-cert.pem
  kubectl get $NS secret $CERT_NAME -o jsonpath='{.data.key\.pem}' |base64 -d  > key.pem

  echo "Generated cert-chain.pem, root-cert.pem and key.pem. Please install them on /etc/certs"
  echo "The directory and files must be owned by 'istio-proxy' user"
}


# Install required files on a VM and run the setup script.
# This is an example to help integrating the steps into the admin automation tools.
#
# Must be run for each VM added to the cluster
# Params:
# - name of the VM - used to copy files over.
# - optional service account to be provisioned (defaults to istio.default)
# - optional namespace of the service account and VM services, defaults to SERVICE_NAMESPACE env
# or kube config.
#
# Expected to be run from the release directory (ie istio-0.2.8/ or istio/)
function istioBootstrapVM() {
 local NAME=${1}

 local SA=${2:-${SERVICE_ACCOUNT:-istio.default}}
 local NS=${3:-${SERVICE_NAMESPACE:-}}

 istio_provision_certs $SA $NS

 # Copy deb, helper and config files
 # Reviews not copied - VMs don't support labels yet.
 istioCopy $NAME \
   kubedns \
   *.pem \
   cluster.env \
   install/tools/setupIstioVM.sh \

 # Run the setup script.
 istioRun $NAME "sudo bash -c -x ./setupIstioVM.sh"
}


# Helper functions for the main script

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

if [[ ${1:-} == "generateDnsmasq" ]] ; then
  istioDnsmasq
elif [[ ${1:-} == "generateClusterEnv" ]] ; then
  shift
  istioClusterEnv $1
elif [[ ${1:-} == "machineCerts" ]] ; then
  shift
  istio_provision_certs $1 $2
elif [[ ${1:-} == "machineSetup" ]] ; then
  shift
  istioBootstrapVM $1
else
  echo "$0 generateDnsmasq: Generate dnsmasq config files (one time)"
  echo "GCP_OPTS=\"--project P --zone Z\" $0 generateClusterEnv K8S_CLUSTER_NAME: Generate cluster range config files (one time)"
  echo "$0 machineCerts SERVICE_ACCOUNT: Generate bootstrap machine certs. Uses 'default' account if no parameters (one time per host)"
  echo "$0 machineSetup HOST: Copy files to HOST, and run the setup script (one time per host)"
fi
