#!/bin/bash

set -ex

echo foo > /dev/null

#ISTIO=$HOME/istio-releases/istio-1.4-alpha.c75a0e07a0f59b5ffcfa1cd3472d77144a4e3e91/
ISTIO=/usr/local/google/home/jasonyoung/istio-releases/istio-1.3.2/

config=mesh-pair.yaml

gen() {
  for context in $(kubectl config get-contexts -o name); do
    (
      echo "--- context = "${context}" --- (GEN)"

      valuefile="values-${context?}.yaml"
      istioctl --context ${context?} x join -f ${config} --gen=2 > ${valuefile}
    )&
  done
  wait
}

init() {
  for context in $(kubectl config get-contexts -o name); do
    kc() { kubectl --context ${context} $@; }
    (

      echo "--- context = "${context}" --- (INIT)"

      kc delete istio-io --all

      sleep 1

      valuefile="values-${context?}.yaml"

      kc create namespace istio-system >/dev/null || true

      helm template ${ISTIO}install/kubernetes/helm/istio-init \
        --name istio-init \
        --namespace istio-system \
        -f ${valuefile} | kc apply -f -
    )&
  done
  wait
}

install() {
  for context in $(kubectl config get-contexts -o name); do
    kc() { kubectl --context ${context} $@; }
    (
      echo "--- context = "${context}" --- (INSTALL)"

      valuefile="values-${context?}.yaml"
      kc -n istio-system delete job --all

      helm template ${ISTIO}install/kubernetes/helm/istio \
        --name istio \
        --namespace istio-system \
        -f ${valuefile} | kubectl --context=${context?} apply -f -
    )&
  done
  wait
}

discovery() {
  for context in $(kubectl config get-contexts -o name); do
    (
      echo "--- context = "${context}" --- (JOIN)"
      istioctl --context=${context?} x join -f ${config} --discovery=2
    )&
  done
  wait
}

rootCA() {
  pushd $HOME/multicluster/setup/certs
  rm *.pem
  rm root*
  make root-ca
  popd
}

intermediateCA() {
  pushd $HOME/multicluster/setup/certs

  for context in $(kubectl config get-contexts -o name); do
    kc() { kubectl --context ${context} $@; }
    (
      make ${context}-certs
      pushd ${context}
      kubectl --context ${context?} create namespace istio-system || true
      kubectl --context=${context?} create secret generic cacerts -n istio-system \
        --from-file=ca-cert.pem \
        --from-file=ca-key.pem \
        --from-file=root-cert.pem \
        --from-file=cert-chain.pem \
        --dry-run -o yaml | kc apply -f -
      popd
    )&
  done
  popd
  wait
}

ingress() {
  set +x
  for context in $(kubectl config get-contexts -o name); do
    kc() { kubectl --context ${context} $@; }
    (
      IP=$(kc -n istio-system get service istio-ingressgateway -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
      echo "${context} => http://${IP}/productpage"
    )&
  done
  set -x
  wait
}

app() {
  for context in $(kubectl config get-contexts -o name); do
    kc() { kubectl --context ${context} $@; }
    (
      kc label namespace default istio-injection=enabled --overwrite
      kc apply -f ${ISTIO}/samples/bookinfo/platform/kube/bookinfo.yaml
      kc apply -f ${ISTIO}/samples/bookinfo/networking/bookinfo-gateway.yaml
    )&
  done
  wait
}

test() {
  for context in $(kubectl config get-contexts -o name); do
    kc() { kubectl --context ${context} $@; }

    kc cluster-info
  done
}

down() {
  for context in $(kubectl config get-contexts -o name); do
    kc() { kubectl --context ${context} $@; }
    (
      for d in $(kc get deployment -o name); do
        kc scale ${d} --replicas=0
      done
    )&
  done
  wait
}

#test

# rootCA
# intermediateCA

#gen
#init
# sleep 3
#install
#discovery
#app
#ingress

# down
