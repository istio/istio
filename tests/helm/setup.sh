#/bin/bash

function testIstioSystem() {
   pushd $TOP/src/istio.io/istio
   helm -n istio-system template \
    --values tests/helm/values-istio-test.yaml \
    --set global.refreshInterval=30s \
    --set global.tag=$TAG \
    --set global.hub=$HUB \
    install/kubernetes/helm/istio  | \
        kubectl apply -n istio-system -f -
   popd
}

# Install istio
function testInstall() {
    make istio-demo.yaml
    kubectl create ns istio-system
    testIstioSystem

    kubectl -n test apply -f samples/httpbin/httpbin.yaml

    kubectl create ns test
    kubectl label namespace test istio-injection=enabled

    kubectl create ns bookinfo
    kubectl label namespace bookinfo istio-injection=enabled
    kubectl -n bookinfo apply -f samples/bookinfo/kube/bookinfo.yaml
}

# Apply the helm template
function testApply() {
   pushd $TOP/src/istio.io/istio
   helm -n test template tests/helm |kubectl -n test apply -f -
   popd
}

function testCreateDNS() {
    D=${1-.istio.webinf.info.}
    P=${2}
    gcloud dns --project=costin-istio record-sets transaction start --zone=istiotest

    gcloud dns --project=costin-istio record-sets transaction add ingress08.$D --name=grafana.v08.$D --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.$D --name=prom.v08.$D --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.$D --name=fortio2.v08.$D --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.$D --name=pilot.v08.$D --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.$D --name=fortio.v08.$D --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.$D --name=fortioraw.v08.$D --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.$D --name=bookinfo.v08.$D --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.$D --name=httpbin.v08.$D --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.$D --name=citadel.v08.$D --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.$D --name=mixer.v08.$D --ttl=300 --type=CNAME --zone=istiotest

    gcloud dns --project=costin-istio record-sets transaction execute --zone=istiotest
}

