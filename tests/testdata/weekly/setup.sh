#/bin/bash

function install() {
    make istio-demo.yaml
    kubectl create ns istio-system
    kubectl apply -n istio-system -f install/kubernetes/istio-demo.yaml

    kubectl create ns raw
    kubectl -n raw apply -f tests/testdata/weekly/app-fortio.yaml
    kubectl -n raw apply -f tests/testdata/weekly/iperf3.yaml
    kubectl -n raw apply -f samples/httpbin/httpbin.yaml
    # To evaluate raw, direct LB to fortio/iperf
    kubectl -n raw apply -f tests/testdata/weekly/fortioilb.yaml

    kubectl create ns test
    kubectl label namespace test istio-injection=enabled
    kubectl -n test apply -f tests/testdata/weekly/app-fortio.yaml
    kubectl -n test apply -f tests/testdata/weekly/iperf3.yaml
    kubectl -n test apply -f samples/httpbin/httpbin.yaml

    kubectl create ns bookinfo
    kubectl label namespace bookinfo istio-injection=enabled
    kubectl -n bookinfo apply -f samples/bookinfo/kube/bookinfo.yaml

    # DNS entries created separatedly
    kubectl -n raw apply -f tests/testdata/weekly/istio08.yaml

}

function createDNS() {
        gcloud dns --project=costin-istio record-sets transaction start --zone=istiotest

    gcloud dns --project=costin-istio record-sets transaction add ingress08.istio.webinf.info. --name=grafana.v08.istio.webinf.info. --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.istio.webinf.info. --name=prom.v08.istio.webinf.info. --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.istio.webinf.info. --name=fortio2.v08.istio.webinf.info. --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.istio.webinf.info. --name=pilot.v08.istio.webinf.info. --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.istio.webinf.info. --name=fortio.v08.istio.webinf.info. --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.istio.webinf.info. --name=fortioraw.v08.istio.webinf.info. --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.istio.webinf.info. --name=bookinfo.v08.istio.webinf.info. --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.istio.webinf.info. --name=httpbin.v08.istio.webinf.info. --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.istio.webinf.info. --name=citadel.v08.istio.webinf.info. --ttl=300 --type=CNAME --zone=istiotest
    gcloud dns --project=costin-istio record-sets transaction add ingress08.istio.webinf.info. --name=mixer.v08.istio.webinf.info. --ttl=300 --type=CNAME --zone=istiotest

    gcloud dns --project=costin-istio record-sets transaction execute --zone=istiotest
}

