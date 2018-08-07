#!/bin/bash

vm_driver="kvm2"

case "${OSTYPE}" in
  darwin*) vm_driver="hyperkit";;
  linux*)
    DISTRO="$(lsb_release -i -s)"
    case "${DISTRO}" in
      Debian|Ubuntu)
        vm_driver="kvm2";;
      *) echo "unsupported distro: ${DISTRO}" ;;
    esac;;
  *) echo "unsupported: ${OSTYPE}" ;;
esac

echo "Using $vm_driver as VM for Minikube."

# Delete any previous minikube cluster
minikube delete

echo "Starting Minikube."

# Start minikube
minikube start \
    --extra-config=controller-manager.cluster-signing-cert-file="/var/lib/localkube/certs/ca.crt" \
    --extra-config=controller-manager.cluster-signing-key-file="/var/lib/localkube/certs/ca.key" \
    --extra-config=apiserver.admission-control="NamespaceLifecycle,LimitRanger,ServiceAccount,PersistentVolumeLabel,DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota" \
    --kubernetes-version=v1.10.0 \
    --insecure-registry="localhost:5000" \
    --cpus=4 \
    --memory=8192 \
    --vm-driver=$vm_driver

#Setup docker to talk to minikube
eval "$(minikube docker-env)"

while ! kubectl get pods -n kube-system | grep kube-proxy |  grep Running > /dev/null; do
  echo "kube-proxy not ready, will check again in 5 sec"
  sleep 5
  kubectl get pods -n kube-system |  grep kube-proxy | grep Running > /dev/null
done

# Set up env ISTIO if not done yet
if [[ -z "${ISTIO// }" ]]; then
  if [[ -z "${GOPATH// }" ]]; then
    echo GOPATH is not set. Please set and run script again.
    exit
  fi
  export ISTIO=$GOPATH/src/istio.io
  echo 'Set ISTIO to' "$ISTIO"
fi

#Setup LocalRegistry
if [ ! -f "$ISTIO/istio/tests/util/localregistry/localregistry.yaml" ]; then
    echo File "$ISTIO/istio/tests/util/localregistry/localregistry.yaml" not found!.
    echo Please make sure "$ISTIO" points to your Istio codebase.
    echo See https://github.com/istio/istio/wiki/Dev-Guide#setting-up-environment-variables
    exit
fi
kubectl apply -f "$ISTIO/istio/tests/util/localregistry/localregistry.yaml"
echo "local registry started"

while ! kubectl get pods -n kube-system | grep kube-registry-v0 | grep Running > /dev/null; do
  echo "kube-registry-v0 not ready, will check again in 5 sec"
  sleep 5
  kubectl get pods -n kube-system | grep kube-registry-v0 | grep Running > /dev/null
done

#Setup port forwarding
echo "Setting up port forwarding"
POD=$(kubectl get po -n kube-system | grep kube-registry-v0 | awk '{print $1;}')
kubectl port-forward --namespace kube-system "$POD" 5000:5000 &

echo "Host Setup Completed"
