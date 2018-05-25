#!/bin/bash

# Start minikube
minikube start \
    --extra-config=controller-manager.cluster-signing-cert-file="/var/lib/localkube/certs/ca.crt" \
    --extra-config=controller-manager.cluster-signing-key-file="/var/lib/localkube/certs/ca.key" \
    --extra-config=apiserver.admission-control="NamespaceLifecycle,LimitRanger,ServiceAccount,PersistentVolumeLabel,DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota" \
    --kubernetes-version=v1.10.0 \
    --vm-driver=kvm2 

#Setup docker to talk to minikube
eval $(minikube docker-env)


kubectl get pods -n kube-system | grep Running > /dev/null
while [ $? -ne 0 ]; do
  kubectl get pods -n kube-system | grep Running > /dev/null
done

#Setup LocalRegistry
kubectl apply -f $ISTIO/istio/tests/util/localregistry/localregistry.yaml
echo "local registry started"

kubectl get pods -n kube-system | grep kube-registry-v0 | grep Running > /dev/null
while [ $? -ne 0 ]; do
  kubectl get pods -n kube-system | grep kube-registry-v0 | grep Running > /dev/null
done

#Setup port forwarding
POD=`kubectl get po -n kube-system | grep kube-registry-v0 | awk '{print $1;}'`
kubectl port-forward --namespace kube-system $POD 5000:5000 &

# Set up env ISTIO if not done yet
if [[ -z "${ISTIO// }" ]]; then
    export ISTIO=$GOPATH/src/istio.io
    echo 'Set ISTIO to' $ISTIO
fi
