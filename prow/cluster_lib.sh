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
USE_GKE=${USE_GKE:-True}

if [[ "$USE_GKE" == "True" ]]; then
  IFS=';'
  VERSIONS=($(gcloud container get-server-config --project=${PROJECT_NAME} --zone=${ZONE} --format='value(validMasterVersions)'))
  unset IFS
  CLUSTER_VERSION="${VERSIONS[0]}"
else
  CLUSTER_VERSION=$(kubectl version --short | grep Server | awk  '{ print $3 }')
fi

KUBE_USER="${KUBE_USER:-istio-prow-test-job@istio-testing.iam.gserviceaccount.com}"
CLUSTER_CREATED=false

function setup_clusterreg () {
    # setup cluster-registries dir setup by mason
    CLUSTERREG_DIR=${CLUSTERREG_DIR:-$(mktemp -d /tmp/clusterregXXX)}

    # mason dumps all the kubeconfigs into the same file but we need to use per cluster
    # files for the clusterregsitry config.  Create the separate files.
    #  -- if PILOT_CLUSTER not set, assume pilot install to be in the first cluster
    PILOT_CLUSTER=${PILOT_CLUSTER:-$(kubectl config current-context)}
    unset IFS
    k_contexts=$(kubectl config get-contexts -o name)
    for context in $k_contexts; do
        if [[ "$PILOT_CLUSTER" != "$context" ]]; then
            kubectl config use-context ${context}
            kubectl config view --raw=true --minify=true > ${CLUSTERREG_DIR}/${context}.kconf
        fi
    done
    kubectl config use-context $PILOT_CLUSTER
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
  # use current-context if pilot_cluster not set
  PILOT_CLUSTER=${PILOT_CLUSTER:-$(kubectl config current-context)}

  unset IFS
  k_contexts=$(kubectl config get-contexts -o name)
  for context in $k_contexts; do
     kubectl config use-context ${context}

     kubectl create clusterrolebinding prow-cluster-admin-binding\
       --clusterrole=cluster-admin\
       --user="${KUBE_USER}"
  done
  if [[ "$SETUP_CLUSTERREG" == "True" ]]; then
      setup_clusterreg
  fi
  kubectl config use-context $PILOT_CLUSTER

  if [[ "$USE_GKE" == "True" ]]; then
    ALL_CLUSTER_CIDRS=
    for cidr in $(gcloud container clusters list --format='value(clusterIpv4Cidr)'); do
      if [[ "$ALL_CLUSTER_CIDRS" != "" ]]; then
        ALL_CLUSTER_CIDRS+=','
      fi
      ALL_CLUSTER_CIDRS+=$cidr
    done
    ALL_CLUSTER_NETTAGS=
    for net_tag in $(gcloud compute instances list --format=json | jq '.[].tags.items[0]' | tr -d '"'); do
      if [[ "$ALL_CLUSTER_NETTAGS" =~ .*"$net_tag".* ]]; then
        # tag isn't unique so don't add
        echo "$net_tag isn't unique"
      else
        if [[ "$ALL_CLUSTER_NETTAGS" != "" ]]; then
          ALL_CLUSTER_NETTAGS+=','
        fi
        ALL_CLUSTER_NETTAGS+=$net_tag
      fi
    done
    gcloud compute firewall-rules create istio-multicluster-test-pods --allow=tcp,udp,icmp,esp,ah,sctp --direction=INGRESS --priority=900 --source-ranges="$ALL_CLUSTER_CIDRS" --target-tags=$ALL_CLUSTER_NETTAGS --quiet
  fi
}

function unsetup_clusters() {
  # use current-context if pilot_cluster not set
  PILOT_CLUSTER=${PILOT_CLUSTER:-$(kubectl config current-context)}

  unset IFS
  k_contexts=$(kubectl config get-contexts -o name)
  for context in $k_contexts; do
     kubectl config use-context ${context}

     kubectl delete clusterrolebinding prow-cluster-admin-binding
  done
  kubectl config use-context $PILOT_CLUSTER
  if [[ "$USE_GKE" == "True" ]]; then
     gcloud compute firewall-rules delete istio-multicluster-test-pods --quiet
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

