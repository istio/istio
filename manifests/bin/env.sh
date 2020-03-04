#!/usr/bin/env bash

# Copyright Istio Authors
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

# A set of helper functions and examples of install. You can set ISTIO_CONFIG to a yaml file containing
# your own setting overrides.
#
# - iop_FOO [update|install|delete] - will update(default)  or install/delete the FOO component
# - iop_all - a typical deployment with all core components.
#
#
# Environment:
# - ISTIO_CONFIG - file containing user-specified overrides
# - TOP - if set will be used to locate src/istio.io/istio for installing a 'bookinfo-style' istio for upgrade tests
# - TEMPLATE=1 - generate template to stdout instead of installing
# - INSTALL=1: do an install instead of the default 'update'
# - DELETE=1: do a delete/purge instead of the default 'update'
# - NAMESPACE - namespace where the component is installed, defaults to name of component
# - DOMAIN - if set, ingress will setup mappings for the domain (requires a A and * CNAME records)
#
# Files:
# global.yaml - istio common settings and docs
# FOO/values.yaml - each component settings (not including globals)
# ~/.istio.rc - environment variables sourced - may include TOP, TAG, HUB
# ~/.istio-values.yaml - user config (can include common setting overrides)
#
# --recreate-pods will force pods to restart, even if no config was changed, to pick the new label


# Allow setting some common per user env.
if [ -f "$HOME"/.istio.rc ]; then
    # shellcheck disable=SC1090
    source "$HOME"/.istio.rc
fi

if [ "$TOP" == "" ]; then
  BASE=.
else
  BASE=$TOP/src/istio.io/installer
fi

# Aliases for common kubectl
alias kis='kubectl -n istio-system'
alias kic='kubectl -n istio-control'
alias kii='kubectl -n istio-ingress'


# Helper - kubernetes log wrapper
#
# Params:
# - namespace
# - label ( typically app=NAME or release=NAME)
# - container - defaults to istio-proxy
#
function klog() {
    local ns=${1}
    local label=${2}
    local container=${3}
    shift; shift; shift

    kubectl --namespace="$ns" logs "$(kubectl --namespace="$ns" get -l "$label" pod -o=jsonpath='{.items[0].metadata.name}')" "$container" "$*"
}

# Kubernetes exec wrapper
# - namespace
# - label (app=fortio)
# - container (istio-proxy)
function kexec() {
    local ns=$1
    local label=$2
    local container=$3
    shift; shift; shift

    kubectl --namespace="$ns" exec -it "$(kubectl --namespace="$ns" get -l "$label" pod -o=jsonpath='{.items[0].metadata.name}')" -c "$container" -- "$*"
}

# Forward port - Namespace, label, PortLocal, PortRemote
# Example:
#  kfwd istio-system istio=pilot istio-ingress 4444 8080
function kfwd() {
    local NS=$1
    local L=$2
    local PL=$3
    local PR=$4

    local N=$NS-$L
    if [[ -f ${LOG_DIR:-/tmp}/fwd-$N.pid ]] ; then
        kill -9 "$(cat "${LOG_DIR:-/tmp}"/fwd-"$N".pid)"
    fi
    kubectl --namespace="$NS" port-forward "$(kubectl --namespace="$NS" get -l "$L" pod -o=jsonpath='{.items[0].metadata.name}')" "$PL":"$PR" &
    echo $! > "${LOG_DIR:-/tmp}"/fwd-"$N".pid
}

function logs-gateway() {
    istioctl proxy-status -i istio-system
    klog istio-system app=ingressgateway istio-proxy "$*"
}

function exec-gateway() {
    kexec istio-system app=ingressgateway istio-proxy  "$*"
}
function logs-ingress() {
    istioctl proxy-status -i istio-system
    klog istio-system app=ingressgateway istio-proxy "$*"
}
function exec-ingress() {
    kexec istio-system app=ingressgateway istio-proxy  "$*"
}

function logs-inject() {
    klog istio-system istio=sidecar-injector sidecar-injector-webhook "$*"
}

function logs-pilot() {
    klog istio-system istio=pilot discovery  "$*"
}

function logs-fortio() {
    klog fortio11 app=fortiotls istio-proxy "$*"
}

function exec-fortio11-cli-proxy() {
    # curl -v  -k  --key /etc/certs/key.pem --cert /etc/certs/cert-chain.pem https://fortiotls:8080
    kexec fortio11 app=cli-fortio-tls istio-proxy "$*"
}

function iop_test_apps() {
    iop none none "${BASE}"/test/none "$*"
    kubectl create ns httpbin
    kubectl -n httpbin apply -f "${BASE}"/test/k8s/httpbin.yaml
}

# Prepare GKE for Lego DNS. You must have a domain, $DNS_PROJECT
# and a zone DNS_ZONE created.
function getCertLegoInit() {
 # GCP_PROJECT=costin-istio

 gcloud iam service-accounts create dnsmaster

 gcloud projects add-iam-policy-binding "$GCP_PROJECT"  \
   --member "serviceAccount:dnsmaster@${GCP_PROJECT}.iam.gserviceaccount.com" \
   --role roles/dns.admin

 gcloud iam service-accounts keys create "$HOME"/.ssh/dnsmaster.json \
    --iam-account dnsmaster@"${GCP_PROJECT}".iam.gserviceaccount.com

}

# Get a wildcard ACME cert. MUST BE CALLED BEFORE SETTING THE CNAME
function getCertLego() {
 # GCP_PROJECT=costin-istio
 # DOMAIN=istio.webinf.info
 # NAMESPACE - where to create the secret

 #gcloud dns record-sets list --zone ${DNS_ZONE}

 GCE_SERVICE_ACCOUNT_FILE=~/.ssh/dnsmaster.json \
 lego -a --email="dnsmaster@${GCP_PROJECT}.iam.gserviceaccount.com"  \
 --domains="*.${DOMAIN}"     \
 --dns="gcloud"     \
 --path="${HOME}/.lego"  run

 kubectl create -n "${NAMESPACE:-istio-ingress}" secret tls istio-ingressgateway-certs --key "${HOME}"/.lego/certificates/_."${DOMAIN}".key \
    --cert "${HOME}"/.lego/certificates/_."${DOMAIN}".crt

}

# Setup DNS entries - currently using gcloud
# Requires GCP_PROJECT, DOMAIN and DNS_ZONE to be set
# For example, DNS_DOMAIN can be istio.example.com and DNS_ZONE istiozone.
# You need to either buy a domain from google or set the DNS to point to gcp.
# Similar scripts can setup DNS using a different provider
function testCreateDNS() {
    # TODO: cleanup, pretty convoluted
    # GCP_PROJECT=costin-istio DOMAIN=istio.webinf.info IP=35.222.25.73 testCreateDNS control
    # will create ingresscontrol and *.control CNAME.
    local ns=$1

    local sub=${2:-$ns}

    IP=$(kubectl get -n "$ns" service ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
    echo "Gateway IP: $IP"


    gcloud dns --project="$GCP_DNS_PROJECT" record-sets transaction start --zone="$DNS_ZONE"

    gcloud dns --project="$GCP_DNS_PROJECT" record-sets transaction add \
        "$IP" --name=ingress-"${ns}"."${DOMAIN}". \
        --ttl=300 --type=A --zone="$DNS_ZONE"

    gcloud dns --project="$GCP_DNS_PROJECT" record-sets transaction add \
        ingress-"${ns}"."${DOMAIN}". --name="*.${sub}.${DOMAIN}." \
        --ttl=300 --type=CNAME --zone="$DNS_ZONE"

    gcloud dns --project="$GCP_DNS_PROJECT" record-sets transaction execute --zone="$DNS_ZONE"
}



function istio-restart() {
    local L=${1:-app=pilot}
    local NS=${2:-istio-system}

    kubectl --namespace="$NS" delete po -l "$L"
}


# For testing the config. Will start a pilot (using the build directory), with config from k8s.
function localPilot() {
    PID=${LOG_DIR:-/tmp}/pilot.pid

    if [[ -f  $PID ]] ; then
        kill -9 "$(cat "${PID}")"
    fi
    "${TOP}"/out/linux_amd64/release/pilot-discovery discovery \
        --kubeconfig "$KUBECONFIG" \
        --meshConfig test/simple/mesh.yaml \
        --networksConfig test/simple/meshNetworks.yaml &

    echo $! > "${PID}"
}

# For testing the config of sidecar
function localSidecar() {
    BINDIR=${TOP}/out/linux_amd64/release
    "${BINDIR}"/pilot-agent proxy sidecar \
        --domain simple-micro.svc.cluster.local \
        --configPath "${TOP}"/out \
        --binaryPath "${BINDIR}"/envoy \
        --templateFile "${TOP}"/src/istio.io/istio/tools/packaging/common/envoy_bootstrap_v2.json \
        --serviceCluster echosrv.simple-micro \
        --drainDuration 45s --parentShutdownDuration 1m0s \
        --discoveryAddress localhost:15010 \
        --proxyLogLevel=debug \
        --proxyComponentLogLevel=misc:info \
        --connectTimeout 10s \
        --proxyAdminPort 15000 \
        --concurrency 2 \
        --controlPlaneAuthPolicy NONE \
        --statusPort 15020 \
        --controlPlaneBootstrap=false

}

# Fetch the certs from a namespace, save to /etc/cert
# Same process used for mesh expansion, can also be used for dev machines.
function getCerts() {
    local NS=${1:-default}
    local SA=${2:-default}

    kubectl get secret istio."$SA" -n "$NS" -o "jsonpath={.data['key\.pem']}" | base64 -d > /etc/certs/key.pem
    kubectl get secret istio."$SA" -n "$NS" -o "jsonpath={.data['cert-chain\.pem']}" | base64 -d > /etc/certs/cert-chain.pem
    kubectl get secret istio."$SA" -n "$NS" -o "jsonpath={.data['root-cert\.pem']}" | base64 -d > /etc/certs/root-cert.pem
}

# For debugging, get the istio CA. Can be used with openssl or other tools to generate certs.
function getCA() {
    kubectl get secret istio-ca-secret -n istio-system -o "jsonpath={.data['ca-cert\.pem']}" | base64 -d > /etc/certs/ca-cert.pem
    kubectl get secret istio-ca-secret -n istio-system -o "jsonpath={.data['ca-key\.pem']}" | base64 -d > /etc/certs/ca-key.pem
}

function istio_status() {
    echo "=== 1.1"
    istioctl -i istio-system proxy-status
    echo "=== master"
    istioctl -i istio-master proxy-status
    echo "=== micro-ingress"
    istioctl -i istio-ingress proxy-status
}

# Get config
#
# - cmd (routes, listeners, endpoints, clusters)
# - deployment (ex. ingressgateway)
#
# Env: ISTIO_ENV = which pilot to use ( istio-system, istio-master, istio-ingress, ...)
function istio_cfg() {
    local env=${ISTIO_ENV:-istio-system}
    local cmd=$1
    shift
    local dep=$1
    shift


    istioctl -i "$env" proxy-config "$cmd" "$(istioctl -i "$env" proxy-status | grep "$dep" | cut -d' ' -f 1)" "$*"
}

