#!/bin/bash

vm_driver="kvm2"

case "${OSTYPE}" in
  darwin*) vm_driver="hyperkit";;
  linux*)
    ;;
  *) echo "unsupported: ${OSTYPE}" ;;
esac

# Virtual machine driver, the default value is decided by your OS type if it's not specified,
# e.g.:
#   hyperkit default to darwin os
#   kvm2 default to Debian or Ubuntu os
# Besides, you can set any vm-driver you like via exporting `VM_DRIVER` for your environment.
VM_DRIVER=${VM_DRIVER:-${vm_driver}}

echo "Using ${VM_DRIVER} as VM for Minikube."

# Delete any previous minikube cluster
minikube delete

echo "Starting Minikube."

# Start minikube
# When minikube runs in `--vm-driver=none` mode, it requires root permission.
sudo -E minikube start \
    --extra-config=controller-manager.cluster-signing-cert-file="/var/lib/localkube/certs/ca.crt" \
    --extra-config=controller-manager.cluster-signing-key-file="/var/lib/localkube/certs/ca.key" \
    --extra-config=apiserver.admission-control="NamespaceLifecycle,LimitRanger,ServiceAccount,PersistentVolumeLabel,DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota" \
    --kubernetes-version=v1.10.0 \
    --insecure-registry="localhost:5000" \
    --cpus=4 \
    --memory=8192 \
    --vm-driver="$VM_DRIVER"

#Setup docker to talk to minikube
eval "$(minikube docker-env)"

while ! kubectl get pods -n kube-system | grep kube-proxy |  grep Running > /dev/null; do
  echo "kube-proxy not ready, will check again in 5 sec"
  sleep 5
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

echo "Host Setup Completed"
