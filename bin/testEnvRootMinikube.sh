#!/usr/bin/env bash

if [ ! -f $GOPATH/bin/minikube ]; then
    curl -Lo $GOPATH/bin/minikube https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64 && chmod +x $GOPATH/bin/minikube
fi
if [ ! -f $GOPATH/bin/kubectl ]; then
    curl -Lo $GOPATH/bin/kubectl https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl && chmod +x $GOPATH/bin/kubectl
fi

export MINIKUBE_WANTUPDATENOTIFICATION=false
export MINIKUBE_WANTREPORTERRORPROMPT=false
export MINIKUBE_HOME=$HOME
export CHANGE_MINIKUBE_NONE_USER=true

export KUBECONFIG=${KUBECONFIG:-$GOPATH/minikube.conf}
export PATH=$GOPATH/bin:$PATH

function waitMinikube() {
    set -ne
    $GOPATH/bin/kubectl cluster-info
    # this for loop waits until kubectl can access the api server that Minikube has created
    for i in {1..150}; do # timeout for 5 minutes
       $GOPATH/bin/kubectl get po &> /dev/null
       if [ $? -ne 1 ]; then
          break
      fi
      sleep 2
    done
    cat $KUBECONFIG
    $GOPATH/bin/kubectl get svc --all-namespaces
    echo "Minikube is running"
}


# Requires sudo ! Start real kubernetes minikube with none driver
function startMinikubeNone() {
    sudo -E $GOPATH/bin/minikube start \
            --extra-config=apiserver.Admission.PluginNames="Initializers,NamespaceLifecycle,LimitRanger,ServiceAccount,DefaultStorageClass,GenericAdmissionWebhook,ResourceQuota" \
            --kubernetes-version=v1.7.5 --vm-driver=none
    sudo -E $GOPATH/bin/minikube update-context
    sudo chown -R $(id -u) $KUBECONFIG $HOME/.minikube
}

function stopMinikube() {
    sudo minikube stop
}

case "$1" in
    start) startMinikubeNone ;;
    stop) stopMinikube ;;
    wait) waitMinikube ;;
esac