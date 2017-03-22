#!/bin/sh

echo "Adding istio-inject function for manually injecting istio runtime proxy into kubernetes resource files"
echo "Usage: isito-inject <yaml-file> | kubectl apply -f -"

function istio-inject() {
    if [ "$#" -ne 1 ]; then
        echo "Usage: istio-inject <kubernetes-yaml-file>"
        return 1
    fi

    hub=docker.io/istio
    tag=2017-03-22-17.30.06
    istioctl kube-inject \
             -f ${1} \
             --initImage=${hub}/init:${tag} \
             --runtimeImage=${hub}/runtime:${tag}
}
