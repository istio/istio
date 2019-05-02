#!/bin/bash

CNI_HUB=${CNI_HUB:-istio}
CNI_TAG=${CNI_TAG:-$1}
CNI_ENABLE=${CNI_ENABLE:-true}

# Dockerfile copies the PR's istio/cni helm chart into /go/helm/istio-cni
chartdir=/go/helm

cd /go/src/istio.io/istio
if [[ "${ISTIO_REMOTE}" != "" ]]; then
    git remote add nonorigin "${ISTIO_REMOTE}"
    git fetch nonorigin
    git checkout "nonorigin/${ISTIO_REMOTE_BRANCH:-master}"
fi

echo "k8s version"
kubectl version

echo "k8s Nodes"
kubectl get nodes

if [[ "${CNI_ENABLE}" == "true" ]]; then
    # Install istio-cni prior to executing the Istio e2e test.  Now that the helm chart for istio/istio no longer
    # depends on the istio-cni chart, we need to explicitly do this as a prereq for installing Istio
    # (the e2e_simple test installs Istio).
    helm template --values ${chartdir}/istio-cni/values.yaml --name=istio-cni --namespace=kube-system --set "excludeNamespaces={}" --set hub="${CNI_HUB}" --set tag="${CNI_TAG}" --set pullPolicy=IfNotPresent --set logLevel="${CNI_LOGLVL:-debug}"  ${chartdir}/istio-cni > istio-cni_install.yaml
    kubectl apply -f istio-cni_install.yaml
fi

echo "k8s: All pods (CNI enabled: ${CNI_ENABLE})"
kubectl get pods --all-namespaces -o wide

HUB=${HUB:-gcr.io/istio-release}
TAG=${TAG:-master-latest-daily}

HUB=${HUB} TAG=${TAG} make istioctl

HUB=${HUB} TAG=${TAG} ENABLE_ISTIO_CNI=${CNI_ENABLE} E2E_ARGS="--kube_inject_configmap=istio-sidecar-injector ${SKIP_CLEAN:+ --skip_cleanup}" make test/local/auth/e2e_simple
