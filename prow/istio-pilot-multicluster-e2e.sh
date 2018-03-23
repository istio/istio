#!/usr/bin/env bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

# set this to the new multicluster-e2e type
RESOURCE_TYPE="${RESOURCE_TYPE:-gke-e2e-test}"
OWNER=istio-pilot-multicluster-e2e

source "prow/istio-pilot-e2e-common.sh"

# setup cluster-registries dir setup by mason
CLUSTERREG_DIR=${CLUSTERREG_DIR:-$HOME/.kube}


# These IP params don't really matter
PilotIP="1.1.1.1"
ClientCidr=1.1.1.0/24
ServerEndpointIP=1.1.1.1

PilotCfgStore=True

# mason dumps all the kubeconfigs into the same file but we need to use per cluster
# files for the clusterregsitry config.  Create the separate files.
#  -- if PILOT_CLUSTER not set, assume pilot install to be in the first cluster
unset IFS
k_contexts=$(kubectl config get-contexts -o name)
for context in $k_contexts; do
    kubectl config use-context ${context}
    kubectl config view --raw=true --minify=true > ${CLUSTERREG_DIR}/${context}.kconf

    if [[ "$PILOT_CLUSTER" == "$context" ]]; then
        PilotCfgStore=True
    elif [[ "$PILOT_CLUSTER" != "" ]]; then
        PilotCfgStore=False
    fi

    CLUSREG_CONTENT+=$(cat <<EOF

---

apiVersion: clusterregistry.k8s.io/v1alpha1
kind: Cluster
metadata:
  name: ${context}
  annotations:
    config.istio.io/pilotEndpoint: "${PilotIP}:9080"
    config.istio.io/platform: "Kubernetes"
    config.istio.io/pilotCfgStore: ${PilotCfgStore}
    config.istio.io/accessConfigFile: ${context}.kconf
spec:
  kubernetesApiEndpoints:
    serverEndpoints:
      - clientCIDR: "${ClientCidr}"
        serverAddress: "${ServerEndpointIP}"
EOF
)

    PilotCfgStore=False
done

echo "$CLUSREG_CONTENT" > ${CLUSTERREG_DIR}/multi_clus.yaml

GIT_SHA=${GIT_SHA:-$TAG}

# Run tests with auth disabled
make depend e2e_pilot HUB="${HUB}" TAG="${GIT_SHA}" TESTOPTS="--cluster-registry-dir $CLUSTERREG_DIR -use-sidecar-injector=false -use-admission-webhook=false -auth_enable=false -v1alpha3=false"
