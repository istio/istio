#!/bin/bash



function testIstioSystem() {
   pushd "$TOP/src/istio.io/istio" || return
   helm -n istio-system template \
    --set global.tag="$TAG" \
    --set global.hub="$HUB" \
    --values tests/helm/values-istio-test.yaml \
    install/kubernetes/helm/istio  | \
        kubectl apply -n istio-system -f -
   popd || return
}

# Install istio
function testInstall() {
    make istio-demo.yaml
    kubectl create ns istio-system
    testIstioSystem

    kubectl create ns test
    kubectl label namespace test istio-injection=enabled

    kubectl -n test apply -f samples/httpbin/httpbin.yaml
    kubectl create ns bookinfo
    kubectl label namespace bookinfo istio-injection=enabled
    kubectl -n bookinfo apply -f samples/bookinfo/kube/bookinfo.yaml
}

# Apply the helm template
function testApply() {
   local F=${1:-"fortio/fortio:latest"}
   pushd "$TOP/src/istio.io/istio" || return
   helm -n test template \
    --set fortioImage="$F" \
    tests/helm |kubectl -n test apply -f -
   popd || return
}

function testApply1() {
    testApply fortio/fortio:1.0.1
}

# Setup DNS entries - currently using gcloud
# Requires DNS_PROJECT, DNS_DOMAIN and DNS_ZONE to be set
# For example, DNS_DOMAIN can be istio.example.com and DNS_ZONE istiozone.
# You need to either buy a domain from google or set the DNS to point to gcp.
# Similar scripts can setup DNS using a different provider
function testCreateDNS() {

    gcloud dns --project="$DNS_PROJECT" record-sets transaction start --zone="$DNS_ZONE"

  #  gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=grafana.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE
    gcloud dns --project="$DNS_PROJECT" record-sets transaction add "ingress10.${DNS_DOMAIN}." --name="prom.v10.${DNS_DOMAIN}." --ttl=300 --type=CNAME --zone="$DNS_ZONE"
    gcloud dns --project="$DNS_PROJECT" record-sets transaction add "ingress10.${DNS_DOMAIN}." --name="fortio2.v10.${DNS_DOMAIN}." --ttl=300 --type=CNAME --zone="$DNS_ZONE"
    gcloud dns --project="$DNS_PROJECT" record-sets transaction add "ingress10.${DNS_DOMAIN}." --name="pilot.v10.${DNS_DOMAIN}." --ttl=300 --type=CNAME --zone="$DNS_ZONE"
    gcloud dns --project="$DNS_PROJECT" record-sets transaction add "ingress10.${DNS_DOMAIN}." --name="fortio.v10.${DNS_DOMAIN}." --ttl=300 --type=CNAME --zone="$DNS_ZONE"
    gcloud dns --project="$DNS_PROJECT" record-sets transaction add "ingress10.${DNS_DOMAIN}." --name="fortioraw.v10.${DNS_DOMAIN}." --ttl=300 --type=CNAME --zone="$DNS_ZONE"
    gcloud dns --project="$DNS_PROJECT" record-sets transaction add "ingress10.${DNS_DOMAIN}." --name="bookinfo.v10.${DNS_DOMAIN}." --ttl=300 --type=CNAME --zone="$DNS_ZONE"
    gcloud dns --project="$DNS_PROJECT" record-sets transaction add "ingress10.${DNS_DOMAIN}." --name="httpbin.v10.${DNS_DOMAIN}." --ttl=300 --type=CNAME --zone="$DNS_ZONE"
    gcloud dns --project="$DNS_PROJECT" record-sets transaction add "ingress10.${DNS_DOMAIN}." --name="citadel.v10.${DNS_DOMAIN}." --ttl=300 --type=CNAME --zone="$DNS_ZONE"
    gcloud dns --project="$DNS_PROJECT" record-sets transaction add "ingress10.${DNS_DOMAIN}." --name="mixer.v10.${DNS_DOMAIN}." --ttl=300 --type=CNAME --zone="$DNS_ZONE"

    gcloud dns --project="$DNS_PROJECT" record-sets transaction execute --zone="$DNS_ZONE"
}

# Run this after adding a new name for ingress testing
function testAddDNS() {
    local N=$1

    gcloud dns --project="$DNS_PROJECT" record-sets transaction start --zone="$DNS_ZONE"

    gcloud dns --project="$DNS_PROJECT" record-sets transaction add "ingress10.${DNS_DOMAIN}." --name="${N}.v10.${DNS_DOMAIN}." --ttl=300 --type=CNAME --zone="$DNS_ZONE"

    gcloud dns --project="$DNS_PROJECT" record-sets transaction execute --zone="$DNS_ZONE"
}
