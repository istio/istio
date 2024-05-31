#!/bin/bash
#
# Copyright Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

SCRIPTDIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

# only ask if in interactive mode
if [[ -t 0 && -z ${NAMESPACE} ]];then
  echo -n "namespace ? [default] "
  read -r NAMESPACE
fi

# verify if the namespace exists, otherwise use default namespace
if [[ -n ${NAMESPACE} ]];then
  ns=$(kubectl get namespace "${NAMESPACE}" --no-headers --output=go-template="{{.metadata.name}}" 2>/dev/null)
  if [[ -z ${ns} ]];then
    echo "NAMESPACE ${NAMESPACE} not found."
    NAMESPACE=default
  fi
fi

# if no namespace is provided, use default namespace
if [[ -z ${NAMESPACE} ]];then
  NAMESPACE=default
fi

echo "using NAMESPACE=${NAMESPACE}"

# clean up Istio traffic management resources that may have been used
protos=( destinationrules virtualservices gateways authorizationpolicies )
for proto in "${protos[@]}"; do
  for resource in $(kubectl get -n "${NAMESPACE}" "$proto" -o name); do
    kubectl delete -n "${NAMESPACE}" "$resource";
  done
done

# clean up Gateway API resources that may have been used
if kubectl get crd gateways.gateway.networking.k8s.io >/dev/null 2>&1; then
  protos=( httproutes gateways.gateway.networking.k8s.io )
  for proto in "${protos[@]}"; do
    for resource in $(kubectl get -n "${NAMESPACE}" "$proto" -o name); do
      kubectl delete -n "${NAMESPACE}" "$resource";
    done
  done
  kubectl delete -n "${NAMESPACE}" -f "$SCRIPTDIR/bookinfo-versions.yaml" >/dev/null 2>&1
fi

OUTPUT=$(mktemp)
export OUTPUT
echo "Application cleanup may take up to one minute"
kubectl delete -n "${NAMESPACE}" -f "$SCRIPTDIR/bookinfo.yaml" > "${OUTPUT}" 2>&1
ret=$?
function cleanup() {
  rm -f "${OUTPUT}"
}

trap cleanup EXIT

if [[ ${ret} -eq 0 ]];then
  cat "${OUTPUT}"
else
  # ignore NotFound errors
  OUT2=$(grep -v NotFound "${OUTPUT}")
  if [[ -n ${OUT2} ]];then
    cat "${OUTPUT}"
    exit ${ret}
  fi
fi

# wait for 30 sec for bookinfo to clean up
sleep 30

echo "Application cleanup successful"
