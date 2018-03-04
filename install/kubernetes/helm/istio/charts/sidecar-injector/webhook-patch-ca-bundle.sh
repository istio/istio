#!/bin/bash

set -e

CA_BUNDLE=$(kubectl get configmap -n kube-system extension-apiserver-authentication -o=jsonpath='{.data.client-ca-file}' | base64 | tr -d '\n')

if [[ $CA_BUNDLE == '' ]]; then
    echo "ERROR: can't get ca-client-cert." >&2
    exit 1
fi

MUTATING_WEBHOOK=$(kubectl get MutatingWebhookConfiguration --all-namespaces -l istio=sidecar-injector -o jsonpath='{.items[0].metadata.name}')

if [[ $MUTATING_WEBHOOK == '' ]]; then
    echo "ERROR: can't get MutatingWebhookConfiguration resource from cluster." >&2
    exit 1
fi

echo '{"webhooks": [{"name": "sidecar-injector.istio.io", "clientConfig": {"caBundle": "'${CA_BUNDLE}'"}}]}' | kubectl patch mutatingwebhookconfiguration ${MUTATING_WEBHOOK} -p "$(cat)"
