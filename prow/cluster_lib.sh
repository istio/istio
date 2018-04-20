#!/bin/bash

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


#######################################
#                                     #
#             e2e-suite               #
#                                     #
#######################################

PROJECT_NAME=istio-testing
ZONE=us-central1-f
MACHINE_TYPE=n1-standard-4
NUM_NODES=${NUM_NODES:-1}
CLUSTER_NAME=

IFS=';'
VERSIONS=($(gcloud container get-server-config --project=${PROJECT_NAME} --zone=${ZONE} --format='value(validMasterVersions)'))
unset IFS
CLUSTER_VERSION="${VERSIONS[0]}"

KUBE_USER="${KUBE_USER:-istio-prow-test-job@istio-testing.iam.gserviceaccount.com}"
CLUSTER_CREATED=false

function setup_clusterreg () {
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
}

function delete_cluster () {
  if [ "${CLUSTER_CREATED}" = true ]; then
    gcloud container clusters delete ${CLUSTER_NAME}\
      --zone ${ZONE}\
      --project ${PROJECT_NAME}\
      --quiet\
      || echo "Failed to delete cluster ${CLUSTER_NAME}"
  fi
}

function setup_cluster() {
  # use the first context in the list if not preset
  if [[ "$PILOT_CLUSTER" == "" ]]; then
    unset IFS
    k_contexts=$(kubectl config get-contexts -o name)
    for context in $k_contexts; do
        if [[ "$PILOT_CLUSTER" == "" ]]; then
            PILOT_CLUSTER=${context}
        fi
    done
  fi
  kubectl config use-context ${PILOT_CLUSTER}

  kubectl create clusterrolebinding prow-cluster-admin-binding\
    --clusterrole=cluster-admin\
    --user="${KUBE_USER}"
  if [[ "$SETUP_CLUSTERREG" == "True" ]]; then
      setup_clusterreg
  fi
}

function check_cluster() {
  for i in {1..10}
  do
    status=$(kubectl get namespace || echo "Unreachable")
    [[ ${status} == 'Unreachable' ]] || break
    if [ ${i} -eq 10 ]; then
      echo "Cannot connect to the new cluster"; exit 1
    fi
    sleep 5
  done
}

