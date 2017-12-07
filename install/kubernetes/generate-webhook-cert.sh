#!/bin/bash

set -e

usage() {
    cat <<EOF
Generate certificate suitable for use with an Istio webhook service.

This script demonstrates how to use the CloudFlare's PKI toolkit
(cfssl) and k8s' CertificateSigningRequest API to a generate a
certificate signed by k8s CA suitable for use with Istio webhook
services. This requires permissions to create and approve CSR. See
https://kubernetes.io/docs/tasks/tls/managing-tls-in-a-cluster for
detailed explantion and additional instructions.

The server key/cert k8s CA cert are stored in a k8s secret.

usage: ${0} [OPTIONS]

The following flags are required.

       --service          Service name of webhook.
       --namespace        Namespace where webhook service resides.
       --secret           Secret name for CA certificate and server certificate/key pair.
       --secret-namespace Namespace where secret should be created. Default
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
        --secret-namespace)
            secretNamespace="$2"
            shift
            ;;
        *)
            usage
            ;;
    esac
    shift
done

[ -z ${service} ] && usage
[ -z ${secret} ] && usage
[ -z ${namespace} ] && usage
[ -z ${secretNamespace} ] && usage

# verify cfssl toolkit is installed
if [ ! -x "$(command -v cfssl)" ]; then
    echo "cfssl not found. See https://kubernetes.io/docs/concepts/cluster-administration/certificates/#cfssl for install instructions."
    exit 1
fi

if [ ! -x "$(command -v cfssljson)" ]; then
    echo "cfssljson not found. See https://kubernetes.io/docs/concepts/cluster-administration/certificates/#cfssl for install instructions."
    exit 1
fi

csrName=${service}.${namespace}
tmpdir=$(mktemp -d)
echo "creating certs in tmpdir ${tmpdir} "

# generate server key
cat <<EOF | cfssl genkey - | cfssljson -bare ${tmpdir}/server
{
  "hosts": [ "${service}.${namespace}.svc"  ],
  "CN": "${service}.${namespace}.svc",
  "key": {
    "algo": "ecdsa",
    "size": 256
  }
}
EOF

# clean-up any previously created CSR for our service. Ignore errors if not present.
kubectl delete csr ${csrName} 2>/dev/null || true

# create  server cert/key CSR and  send to k8s API
cat <<EOF | kubectl create -f -
apiVersion: certificates.k8s.io/v1beta1
kind: CertificateSigningRequest
metadata:
  name: ${csrName}
spec:
  groups:
  - system:authenticated
  request: $(cat ${tmpdir}/server.csr | base64 | tr -d '\n')
  usages:
  - digital signature
  - key encipherment
  - server auth
EOF

# verify CSR has been created
while true; do
    kubectl get csr ${csrName}
    if [ "$?" -eq 0 ]; then
        break
    fi
done

# approve and fetch the signed certificate
kubectl certificate approve ${csrName}
kubectl get csr ${csrName} -o jsonpath='{.status.certificate}' | base64 -d > ${tmpdir}/server-cert.pem

# fetch the CA certificate that signed our certificate
secretToken=$(kubectl -n ${namespace} get serviceaccount default -o jsonpath='{.secrets[0].name}')
kubectl get secret -n ${namespace} ${secretToken} -o jsonpath='{.data.ca\.crt}' | base64 -d > ${tmpdir}/ca-cert.pem

# create the secret with CA cert and server cert/key
kubectl create secret generic ${secret} \
        --from-file=${tmpdir}/ca-cert.pem \
        --from-file=${tmpdir}/server-key.pem \
        --from-file=${tmpdir}/server-cert.pem \
        --dry-run -o yaml |
    kubectl -n ${secretNamespace} apply -f -
