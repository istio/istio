#!/bin/bash

# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -eu # Print out all commands, exit on failed commands

SKIP_CONFIRM=""
ISTIO_REV="asm-managed"
CONTEXT=""

usage() {
    echo "Usage:"
    echo "  $0 [OPTIONS]"
    echo
    echo "  --skip-confirm, -y    select 'yes' for any prompts."
    echo "  --context             context to use for kubectl"
    echo
}

die() {
    echo "$*" >&2 ; exit 1;
}

kube() {
  echo "Running: kubectl $*"
  kubectl --context="${CONTEXT}" "$@"
}

prompt() {
  read -r -p "${1} [y/N] " response
  case "$response" in
      [yY][eE][sS]|[yY])
          return
          ;;
  esac
  false
}

print_intro() {
  if [[ -z "${SKIP_CONFIRM}" ]]; then
    echo
    echo "This tool automatically upgrades the Istio addon in the current cluster from 1.4 to Anthos Service Mesh."
    echo "The cluster is selected by the current kubectl configuration."
    echo
    prompt "Do you want to proceed?" || exit 0
    echo
  fi
}

check_prerequisites() {
  echo "Checking prerequisites:"
  command -v kubectl >| /dev/null 2>&1 || die "kubectl must be installed, aborting."
  echo "OK"
}


disable_galley_webhook() {
  echo "Disabling the Istio validating webhook:"
  kube patch clusterrole -n istio-system istio-galley-istio-system --type='json' -p='[{"op": "replace", "path": "/rules/2/verbs/0", "value": "get"}]'
  kube delete ValidatingWebhookConfiguration istio-galley --ignore-not-found
  echo "OK"
}


replace_gateway() {
  echo "Updating the ingress gateway:"
   kube label namespace istio-system istio-injection- istio.io/rev- --overwrite
  cat <<EOF | kube apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: asm-ingressgateway
  namespace: istio-system
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: asm-ingressgateway
  namespace: istio-system
spec:
  selector:
    matchLabels:
      app: istio-ingressgateway
      istio: ingressgateway
  template:
    metadata:
      annotations:
        inject.istio.io/templates: gateway
      labels:
        # Labels must match the istio-system Service
        istio: ingressgateway
        app: istio-ingressgateway
        release: istio
        istio.io/rev: asm-managed
    spec:
      serviceAccountName: asm-ingressgateway
      containers:
      - name: istio-proxy
        image: auto
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: asm-ingressgateway
  namespace: istio-system
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "watch", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: asm-ingressgateway
  namespace: istio-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: asm-ingressgateway
subjects:
- kind: ServiceAccount
  name: asm-ingressgateway
EOF
  kube wait --for=condition=available --timeout=600s deployment/asm-ingressgateway -n istio-system
  kube -n istio-system patch hpa istio-ingressgateway --patch '{"spec":{"minReplicas":1}}'
  kube -n istio-system scale deployment istio-ingressgateway --replicas=0
  echo "OK"
}

print_restart_instructions() {
  echo
  echo "The first part of the migration completed successfully. Now, please perform the following steps:"
  echo
  echo "1. Label any namespaces with auto injection using the following command:"
  echo
  echo "     kubectl label namespace <NAMESPACE> istio-injection- istio.io/rev=${ISTIO_REV} --overwrite"
  echo
  echo "2. Restart all workloads in these namespaces to update the sidecar proxies to the new version."
  echo
  echo "Any manually injected proxies must also be re-injected manually at this point."
}

while (( "$#" )); do
    PARAM=$(echo "${1}" | awk -F= '{print $1}')
    eval VALUE="$(echo "${1}" | awk -F= '{print $2}')"
    case "${PARAM}" in
        -h | --help)
            usage
            exit
            ;;
        --context)
            CONTEXT=${2?"--context requires a parameter"}
            shift
            ;;
        -y | --skip-confirm)
            SKIP_CONFIRM=true
            ;;
        *)
            echo "ERROR: unknown parameter \"$PARAM\""
            usage
            exit 1
            ;;
    esac
    shift
done

# TODO exit early if we already migrated

check_prerequisites
print_intro
disable_galley_webhook
replace_gateway
print_restart_instructions
