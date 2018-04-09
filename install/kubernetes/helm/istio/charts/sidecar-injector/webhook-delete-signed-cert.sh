#!/bin/bash

set -e

usage() {
    cat <<EOF
Delete certificate that created for Istio webhook service.

usage: ${0} [OPTIONS]

The following flags are required.

       --service          Service name of webhook.
       --namespace        Namespace where webhook service and secret reside.
       --secret           Secret name for CA certificate and server certificate/key pair.
EOF
    exit 1
}

while [[ $# -gt 0 ]]; do
    case ${1} in
        --service)
            service="$2"
            shift
            ;;
        --secret)
            secret="$2"
            shift
            ;;
        --namespace)
            namespace="$2"
            shift
            ;;
        *)
            usage
            ;;
    esac
    shift
done

[ -z ${service} ] && service=istio-sidecar-injector
[ -z ${secret} ] && secret=sidecar-injector-certs
[ -z ${namespace} ] && namespace=istio-system

# clean-up created secret for Istio webhook service.
kubectl delete secret ${secret} -n ${namespace} 2>/dev/null || true

csrName=${service}.${namespace}

# clean-up created CSR for Istio webhook service.
kubectl delete csr ${csrName} 2>/dev/null || true
