#!/bin/bash

set -e

: "${WORKDIR:?WORKDIR not set}"

export KUBECONFIG=${WORKDIR}/mesh.kubeconfig

teardown() {
  local CONTEXT=${1}
  kc() { kubectl --context ${CONTEXT} $@; }



  # wait for galley to die so it doesn't re-create the webhook config.
  kc delete namespace istio-system  --wait --ignore-not-found >/dev/null || true
  kc delete mutatingwebhookconfiguration -l operator.istio.io/managed=Reconcile --ignore-not-found
  kc delete validatingwebhookconfiguration -l operator.istio.io/managed=Reconcile --ignore-not-found

  kc delete clusterrole -l operator.istio.io/managed=Reconcile --ignore-not-found
  kc delete clusterrolebinding -l operator.istio.io/managed=Reconcile --ignore-not-found
  kc delete crd -l operator.istio.io/managed=Reconcile --ignore-not-found

  kc delete -f ../bookinfo/platform/kube/bookinfo.yaml --wait --ignore-not-found
}

for CONTEXT in $(kubectl config get-contexts -o name); do
  (
    echo "tearing down ${CONTEXT}"
    exec 1>/dev/null
    teardown ${CONTEXT}
    exec &>/dev/tty
    echo "finished tearing down ${CONTEXT}"
  )&
  wait
done
