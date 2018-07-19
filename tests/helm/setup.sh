#/bin/bash



function testIstioSystem() {
   pushd $TOP/src/istio.io/istio
   helm -n istio-system template \
    --values tests/helm/values-istio-test.yaml \
    --set global.refreshInterval=30s \
    --set global.tag=$TAG \
    --set global.proxy.accessLogFile="" \
    --set global.proxy.resources.requests.cpu=1100m \
    --set global.proxy.resources.requests.memory=256Mi \
    --set global.imagePullPolicy=Always \
    --set global.hub=$HUB \
    --set gateways.istio-ingressgateway.resources.requests.cpu=1900m \
    --set gateways.istio-ingressgateway.resources.requests.memory=512Mi \
    --set gateways.istio-ingressgateway.resources.limits.cpu=1900m \
    --set gateways.istio-ingressgateway.resources.limits.memory=512Mi \
    install/kubernetes/helm/istio  | \
        kubectl apply -n istio-system -f -
   popd
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
   pushd $TOP/src/istio.io/istio
   helm -n test template \
    tests/helm |kubectl -n test apply -f -
   popd
}

# Setup DNS entries - currently using gcloud
# Requires DNS_PROJECT, DNS_DOMAIN and DNS_ZONE to be set
# For example, DNS_DOMAIN can be istio.example.com and DNS_ZONE istiozone.
# You need to either buy a domain from google or set the DNS to point to gcp.
# Similar scripts can setup DNS using a different provider
function testCreateDNS() {

    gcloud dns --project=$DNS_PROJECT record-sets transaction start --zone=$DNS_ZONE

  #  gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=grafana.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE
    gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=prom.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE
    gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=fortio2.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE
    gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=pilot.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE
    gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=fortio.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE
    gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=fortioraw.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE
    gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=bookinfo.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE
    gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=httpbin.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE
    gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=citadel.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE
    gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=mixer.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE

    gcloud dns --project=$DNS_PROJECT record-sets transaction execute --zone=$DNS_ZONE
}

# Run this after adding a new name for ingress testing
function testAddDNS() {
    local N=$1

    gcloud dns --project=$DNS_PROJECT record-sets transaction start --zone=$DNS_ZONE

    gcloud dns --project=$DNS_PROJECT record-sets transaction add ingress10.${DNS_DOMAIN}. --name=${N}.v10.${DNS_DOMAIN}. --ttl=300 --type=CNAME --zone=$DNS_ZONE

    gcloud dns --project=$DNS_PROJECT record-sets transaction execute --zone=$DNS_ZONE
}