#!/usr/bin/env bash

if [ ! -f /usr/local/bin/minikube ]; then
   curl -Lo minikube https://storage.googleapis.com/minikube/releases/v0.22.3/minikube-linux-amd64 && chmod +x minikube && sudo mv minikube /usr/local/bin/
fi
if [ ! -f /usr/local/bin/kubectl ]; then
   curl -Lo kubectl https://storage.googleapis.com/kubernetes-release/release/v1.7.4/bin/linux/amd64/kubectl && chmod +x kubectl && sudo mv kubectl /usr/local/bin/
fi


export KUBECONFIG=${KUBECONFIG:-$GOPATH/minikube.conf}

function waitMinikube() {
    set +e
    kubectl cluster-info
    # this for loop waits until kubectl can access the api server that Minikube has created
    for i in {1..150}; do # timeout for 5 minutes
       kubectl get po &> /dev/null
       if [ $? -ne 1 ]; then
          break
      fi
      sleep 2
    done
    kubectl get svc --all-namespaces
    echo "Minikube is running"
}


# Requires sudo ! Start real kubernetes minikube with none driver
function startMinikubeNone() {
    export MINIKUBE_WANTUPDATENOTIFICATION=false
    export MINIKUBE_WANTREPORTERRORPROMPT=false
    export MINIKUBE_HOME=$HOME
    export CHANGE_MINIKUBE_NONE_USER=true
    sudo -E minikube start \
            --extra-config=apiserver.Admission.PluginNames="Initializers,NamespaceLifecycle,LimitRanger,ServiceAccount,DefaultStorageClass,GenericAdmissionWebhook,ResourceQuota" \
            --kubernetes-version=v1.7.5 --vm-driver=none
    sudo -E minikube update-context
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