#!/usr/bin/env bash

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
# user-values... - example config overrides
# FOO/values.yaml - each component settings (not including globals)
# ~/.istio.rc - environment variables sourced - may include TOP, TAG, HUB
# ~/.istio-values.yaml - user config (can include common setting overrides)
#
# --recreate-pods will force pods to restart, even if no config was changed, to pick the new label



# Allow setting some common per user env.
if [ -f $HOME/.istio.rc ]; then
    source $HOME/.istio.rc
fi

if [ "$TOP" == "" ]; then
  IBASE=.
else
  IBASE=$TOP/src/github.com/costinm/istio-install
fi

# Contains values overrides for all configs.
# Can point to a different file, based on env or .istio.rc
ISTIO_CONFIG=${ISTIO_CONFIG:-${IBASE}/user-values.yaml}

# Default control plane for advanced installer.
alias kis='kubectl -n istio-system'
alias kic='kubectl -n istio-control'
alias kii='kubectl -n istio-ingress'


# The file contains examples of various setup commands I use on test clusters.


# The examples in this file will create a number of control plane profiles:
#
# istio-control - the main control plane, 1.1 based
# istio-master - based on master
#
# In addition, the istio-ingress and istio-ingress-insecure run their own dedicated pilot, which is needed for
# k8s ingress support and is an example on how to segregate sidecar and gateway pilot (same config - but different replicas)
#
# It creates a number of gateways:
# istio-ingress - k8s ingress + gateway, mtls enabled
# istio-ingress-insecure - k8s ingress + gateway, mtls and certificates off, can only connect to permissive (example for old no-mtls -)
#    Notice the sidecar
# istio-ingress-master - ingress using master control plane
#
# It has 2 telemetry servers:
# istio-telemetry - used for all control planes except istio-master
# istio-telemetry-master - example of separate telemetry server, using istio-master env and accessed by istio-ingress-master.


# Typical installation, similar with istio normal install but using different namespaces/components.
function iop_istio() {

    # Citadel must be in istio-system, where the secrets are stored.
    iop istio-system istio-system-security $IBASE/istio-system-security  $*

    # Galley, Pilot and auto-inject in istio-control. Similar security risks.
    # Can be updated independently, each is optiona.
    iop istio-control istio-config $IBASE/istio-control/istio-config --set configValidation=true
    iop istio-control istio-discovery $IBASE/istio-control/istio-discovery
    iop istio-control istio-autoinject $IBASE/istio-control/istio-autoinject --set enableNamespacesByDefault=true

    iop istio-gateway istio-gateway $IBASE/gateways/istio-ingress

    # Not converted yet to prune
    IOP_MODE=helm iop istio-telemetry istio-telemetry $IBASE/istio-telemetry

    iop istio-policy istio-policy $IBASE/istio-policy
}


# Install a testing environment, based on istio_master
# You can use this as a model to install other versions or flags.
#
# Note that 'istio-env=YOURENV' label or manual injection is needed.
#
# Uses shared (singleton) system citadel.
function iop_master() {
    TAG=master-latest-daily HUB=gcr.io/istio-release iop istio-master istio-config-master $IBASE/istio-control/istio-config

    TAG=master-latest-daily HUB=gcr.io/istio-release iop istio-master istio-discovery-master $IBASE/istio-control/istio-discovery \
       --set policy.enable=false \
       --set global.istioNamespace=istio-master \
       --set global.telemetryNamespace=istio-telemetry-master \
       --set global.policyNamespace=istio-policy-master \
       $*

    TAG=master-latest-daily HUB=gcr.io/istio-release iop istio-master istio-autoinject $IBASE/istio-control/istio-autoinject \
      --set global.istioNamespace=istio-master

    TAG=master-latest-daily HUB=gcr.io/istio-release iop istio-telemetry-master istio-telemetry-master $IBASE/istio-telemetry \
        --set global.istioNamespace=istio-master \
        $*

    TAG=master-latest-daily HUB=gcr.io/istio-release iop istio-gateway-master istio-gateway-master $IBASE/gateways/istio-ingress \
        --set global.istioNamespace=istio-master \
        $*

}


# Example for a minimal install, with an Ingress that supports legacy K8S Ingress and a dedicated pilot.
function iop_k8s_ingress() {

    # No MCP or injector - dedicated for the gateway ( perf and scale characteristics are different from main pilot,
    # and we may want custom settings anyways )
    iop istio-ingress istio-ingress-pilot $IBASE/istio-control/istio-discovery \
         --set ingress.ingressControllerMode=DEFAULT \
         --set env.K8S_INGRESS_NS=istio-ingress \
         --set global.controlPlaneSecurityEnabled=false \
         --set global.mtls.enabled=false \
         --set policy.enabled=false \
         --set useMCP=false
          $*

    # If installing a second ingress, please set "--set ingress.ingressControllerMode=STRICT" or bad things will happen.
     # Also --set ingress.ingressClass=istio-...

     # As an example and to test, ingress is installed using Tiller.
    IOP_MODE=helm iop istio-ingress istio-ingress $IBASE/gateways/istio-ingress \
        --set k8sIngress=true \
        --set global.controlPlaneSecurityEnabled=false \
        --set global.istioNamespace=istio-ingress \
        $*

}


# Optional egress gateway
function iop_egress() {
    iop istio-egress istio-egress $IBASE/gateways/istio-egress $*
}

# Install full istio1.1 in istio-system (using the new script and env)
function iop_istio11_istio_system() {
    iop istio-system istio-system $TOP/src/istio.io/istio/install/kubernetes/helm/istio $*
}

# Install just CNI, in istio-system
# TODO: verify it ignores auto-installed, opt-in possible
function iop_cni() {
    iop istio-system cni $IBASE/optional/istio-cni
}

# Install a load generating namespace
function iop_load() {
    iop load load $IBASE/test/pilotload $*
}


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

    kubectl --namespace=$ns log $(kubectl --namespace=$ns get -l $label pod -o=jsonpath='{.items[0].metadata.name}') $container $*
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

    kubectl --namespace=$ns exec -it $(kubectl --namespace=$ns get -l $label pod -o=jsonpath='{.items[0].metadata.name}') -c $container -- $*
}

# Forward port - Namespace, label, PortLocal, PortRemote
# Example:
#  kfwd istio-control istio=pilot istio-ingress 4444 8080
function kfwd() {
    local NS=$1
    local L=$2
    local PL=$3
    local PR=$4

    local N=$NS-$L
    if [[ -f ${LOG_DIR:-/tmp}/fwd-$N.pid ]] ; then
        kill -9 $(cat $LOG_DIR/fwd-$N.pid)
    fi
    kubectl --namespace=$NS port-forward $(kubectl --namespace=$NS get -l $L pod -o=jsonpath='{.items[0].metadata.name}') $PL:$PR &
    echo $! > $LOG_DIR/fwd-$N.pid
}

function logs-gateway() {
    istioctl proxy-status -i istio-control
    klog istio-gateway app=ingressgateway istio-proxy $*
}

function exec-gateway() {
    kexec istio-gateway app=ingressgateway istio-proxy  $*
}
function logs-ingress() {
    istioctl proxy-status -i istio-control
    klog istio-ingress app=ingressgateway istio-proxy $*
}
function exec-ingress() {
    kexec istio-ingress app=ingressgateway istio-proxy  $*
}

function logs-inject() {
    klog istio-control istio=sidecar-injector sidecar-injector-webhook $*
}

function logs-inject-master() {
    klog istio-master istio=sidecar-injector sidecar-injector-webhook $*
}

function logs-pilot() {
    klog istio-control istio=pilot discovery  $*
}

function logs-fortio() {
    klog fortio11 app=fortiotls istio-proxy $*
}

function exec-fortio11-cli-proxy() {
    # curl -v  -k  --key /etc/certs/key.pem --cert /etc/certs/cert-chain.pem https://fortiotls:8080
    kexec fortio11 app=cli-fortio-tls istio-proxy $*
}

function iop_test_apps() {

    iop fortio-control fortio-control $IBASE/test/fortio --set domain=$DOMAIN $*

    kubectl create ns fortio-nolabel
    iop fortio-nolabel fortio-nolabel $IBASE/test/fortio --set domain=$DOMAIN $*


    iop none none $IBASE/test/none $*

    # Using istio-system (can be pilot10 or pilot11) annotation
    kubectl create ns test
    kubectl label namespace test istio-env=istio-control --overwrite
    # Not yet annotated, prune will fail
    IOP_MODE=helm iop test test test/test


    kubectl create ns bookinfo
    kubectl label namespace bookinfo istio-env=istio-control --overwrite
    kubectl -n bookinfo apply -f $TOP/src/istio.io/samples/bookinfo/kube/bookinfo.yaml

    kubectl create ns bookinfo-master
    kubectl label namespace bookinfo-master istio-env=istio-master --overwrite
    kubectl -n bookinfo apply -f $TOP/src/istio.io/samples/bookinfo/kube/bookinfo.yaml

    kubectl create ns httpbin
    kubectl -n httpbin apply -f $TOP/src/istio.io/samples/httpbin/httpbin.yaml

    #kubectl -n cassandra apply -f test/cassandra
}

# Prepare GKE for Lego DNS. You must have a domain, $DNS_PROJECT
# and a zone DNS_ZONE created.
function getCertLegoInit() {
 # GCP_PROJECT=costin-istio

 gcloud iam service-accounts create dnsmaster

 gcloud projects add-iam-policy-binding $GCP_PROJECT  \
   --member "serviceAccount:dnsmaster@${GCP_PROJECT}.iam.gserviceaccount.com" \
   --role roles/dns.admin

 gcloud iam service-accounts keys create $HOME/.ssh/dnsmaster.json \
    --iam-account dnsmaster@${GCP_PROJECT}.iam.gserviceaccount.com

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

 kubectl create -n ${NAMESPACE:-istio-ingress} secret tls istio-ingressgateway-certs --key ${HOME}/.lego/certificates/_.${DOMAIN}.key \
    --cert ${HOME}/.lego/certificates/_.${DOMAIN}.crt

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
    local ver=${1:-v10}
    local name=ingress${ver}

    gcloud dns --project=$GCP_PROJECT record-sets transaction start --zone=$DNS_ZONE

    gcloud dns --project=$GCP_PROJECT record-sets transaction add $IP --name=${name}.${DOMAIN}. --ttl=300 --type=A --zone=$DNS_ZONE
    gcloud dns --project=$GCP_PROJECT record-sets transaction add ${name}.${DOMAIN}. --name="*.${ver}.${DOMAIN}." \
        --ttl=300  --type=CNAME --zone=$DNS_ZONE

    gcloud dns --project=$GCP_PROJECT record-sets transaction execute --zone=$DNS_ZONE
}



function istio-restart() {
    local L=${1:-app=pilot}
    local NS=${2:-istio-control}

    kubectl --namespace=$NS delete po -l $L
}


# For testing the config
function localPilot() {
    pilot-discovery discovery \
        --kubeconfig $KUBECONIG \
        --meshConfig test/local/mesh.yaml \
        --networksConfig test/local/meshNetworks.yaml

}

# Fetch the certs from a namespace, save to /etc/cert
# Same process used for mesh expansion, can also be used for dev machines.
function getCerts() {
    local NS=${1:-default}
    local SA=${2:-default}

    kubectl get secret istio.$SA -n $NS -o "jsonpath={.data['key\.pem']}" | base64 -d > /etc/certs/key.pem
    kubectl get secret istio.$SA -n $NS -o "jsonpath={.data['cert-chain\.pem']}" | base64 -d > /etc/certs/cert-chain.pem
    kubectl get secret istio.$SA -n $NS -o "jsonpath={.data['root-cert\.pem']}" | base64 -d > /etc/certs/root-cert.pem
}

# For debugging, get the istio CA. Can be used with openssl or other tools to generate certs.
function getCA() {
    kubectl get secret istio-ca-secret -n istio-system -o "jsonpath={.data['ca-cert\.pem']}" | base64 -d > /etc/certs/ca-cert.pem
    kubectl get secret istio-ca-secret -n istio-system -o "jsonpath={.data['ca-key\.pem']}" | base64 -d > /etc/certs/ca-key.pem
}

function istio_status() {
    echo "=== 1.1"
    istioctl -i istio-control proxy-status
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
# Env: ISTIO_ENV = which pilot to use ( istio-control, istio-master, istio-ingress, ...)
function istio_cfg() {
    local env=${ISTIO_ENV:-istio-control}
    local cmd=$1
    shift
    local dep=$1
    shift


    istioctl -i $env proxy-config $cmd $(istioctl -i $env proxy-status | grep $dep | cut -d' ' -f 1) $*
}


# Helper to upgrade or install
#
# Params:
# - namespace
# - release name (helm and kubectl)
# - config template dir
# - [-t] - show template
#
# Env: IOP_MODE=helm - use helm instead of kubectl
# Defaults to kubectl apply --prune
#
# Namespace will be labeled with the value of ISTIO_ENV (default to istio-control)
function iop() {
    local ns=$1
    shift
    local rel=$1
    shift
    local tmpl=$1
    shift

    # Defaults
    local cfg="-f $IBASE/global.yaml"

    # User overrides
    if [ -f $HOME/.istio-values.yaml ]; then
        cfg="$cfg -f $HOME/.istio-values.yaml"
    fi

    if [ "$HUB" != "" ] ; then
        cfg="$cfg --set global.hub=$HUB"
    fi
    if [ "$TAG" != "" ] ; then
        cfg="$cfg --set global.tag=$TAG"
    fi

    if [ "$1" == "-t" ]; then
        shift
        helm template --namespace $ns -n $rel $tmpl  $cfg $*
    elif [ "$1" == "-d" ]; then
        shift
        kubectl delete --namespace $ns -n $rel $tmpl  $cfg $*
    elif [ "$IOP_MODE" == "helm" ] ; then
        echo helm upgrade --wait -i $n $cfg $*
        helm upgrade --namespace $ns --wait -i $rel $tmpl $cfg $*
    else
        # The 'release' tag is used to group related configs.
        # Apply will make sure that any old config with the same tag that is no longer present will
        # be removed. The release MUST be unique across cluster.
        kubectl create  ns $ns > /dev/null  2>&1
        helm template --namespace $ns -n $ns-$rel $tmpl $cfg $* | kubectl apply -n $ns --prune -l release=$ns-$rel -f -
    fi
}

