#!/bin/bash

export K8S_VER=${K8S_VER:-v1.9.2}
export MINIKUBE_VER=${MINIKUBE_VER:-v0.25.0}
set -x

if [ ! -f /usr/local/bin/minikube ]; then
   time curl -Lo minikube https://storage.googleapis.com/minikube/releases/${MINIKUBE_VER}/minikube-linux-amd64 && chmod +x minikube && sudo mv minikube /usr/local/bin/
fi
if [ ! -f /usr/local/bin/kubectl ]; then
   time curl -Lo kubectl https://storage.googleapis.com/kubernetes-release/release/${K8S_VER}/bin/linux/amd64/kubectl && chmod +x kubectl && sudo mv kubectl /usr/local/bin/
fi
if [ ! -f /usr/local/bin/helm ]; then
   curl -Lo helm.tgz https://storage.googleapis.com/kubernetes-helm/helm-v2.8.0-linux-amd64.tar.gz && tar -zxvf helm.tgz && chmod +x linux-amd64/helm && sudo mv linux-amd64/helm /usr/local/bin/
fi


export KUBECONFIG=${KUBECONFIG:-$GOPATH/minikube.conf}

function waitMinikube() {
    set +e
    kubectl cluster-info
    # this for loop waits until kubectl can access the api server that Minikube has created
    for _ in {1..30}; do # timeout for 1 minutes
       kubectl get po --all-namespaces #&> /dev/null
       if [ $? -ne 1 ]; then
          break
      fi
      sleep 2
    done
    if ! kubectl get all --all-namespaces; then
        echo "Kubernetes failed to start"
        ps ax
        netstat -an
        docker images
        cat /var/lib/localkube/localkube.err
        printf '\n\n\n'
        kubectl cluster-info dump
        #exit 1
    fi
    echo "Minikube is running"
}

# Requires sudo ! Start real kubernetes minikube with none driver
function startMinikubeNone() {
    export MINIKUBE_WANTUPDATENOTIFICATION=false
    export MINIKUBE_WANTREPORTERRORPROMPT=false
    export MINIKUBE_HOME=$HOME
    export CHANGE_MINIKUBE_NONE_USER=true
    sudo -E minikube start \
         --kubernetes-version=v1.9.0 \
         --vm-driver=none \
         --extra-config=apiserver.Admission.PluginNames="NamespaceLifecycle,LimitRanger,ServiceAccount,DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota"
    sudo -E minikube update-context
    sudo chown -R "$(id -u)" $KUBECONFIG $HOME/.minikube
}

function stopMinikube() {
    sudo minikube stop
}

case "$1" in
    start) startMinikubeNone ;;
    stop) stopMinikube ;;
    wait) waitMinikube ;;
esac
