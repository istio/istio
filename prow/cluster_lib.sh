#!/bin/bash

# Copyright 2017 Istio Authors
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


#######################################
#                                     #
#             e2e-suite               #
#                                     #
#######################################

KUBE_USER="${KUBE_USER:-istio-prow-test-job@istio-testing.iam.gserviceaccount.com}"
SETUP_CLUSTERREG="${SETUP_CLUSTERREG:-False}"
USE_GKE="${USE_GKE:-True}"
SA_NAMESPACE="istio-system-multi"

function join_by { local IFS="$1"; shift; echo "$*"; }

function join_lines_by_comma() {
  # Turn each line into an element in an array.
  mapfile -t array <<< "$1"
  list=$(join_by , "${array[@]}")
  echo "${list}"
}

function setup_cluster() {
  # use current-context if pilot_cluster not set
  PILOT_CLUSTER="${PILOT_CLUSTER:-$(kubectl config current-context)}"

  unset IFS
  k_contexts=$(kubectl config get-contexts -o name)
  for context in ${k_contexts}; do
     kubectl config use-context "${context}"

     kubectl create clusterrolebinding prow-cluster-admin-binding\
       --clusterrole=cluster-admin\
       --user="${KUBE_USER}"
  done
  if [[ "${SETUP_CLUSTERREG}" == "True" ]]; then
      setup_clusterreg
  fi
  kubectl config use-context "${PILOT_CLUSTER}"

  if [[ "${USE_GKE}" == "True" && "${SETUP_CLUSTERREG}" == "True" ]]; then
    echo "Set up firewall rules."
    date
    ALL_CLUSTER_CIDRS_LINES=$(gcloud container clusters list --format='value(clusterIpv4Cidr)' | sort | uniq)
    ALL_CLUSTER_CIDRS=$(join_lines_by_comma "${ALL_CLUSTER_CIDRS_LINES}")

    ALL_CLUSTER_NETTAGS_LINES=$(gcloud compute instances list --format='value(tags.items.[0])' | sort | uniq)
    ALL_CLUSTER_NETTAGS=$(join_lines_by_comma "${ALL_CLUSTER_NETTAGS_LINES}")

    gcloud compute firewall-rules create istio-multicluster-test-pods \
	    --allow=tcp,udp,icmp,esp,ah,sctp \
	    --direction=INGRESS \
	    --priority=900 \
	    --source-ranges="${ALL_CLUSTER_CIDRS}" \
	    --target-tags="${ALL_CLUSTER_NETTAGS}" --quiet
  fi
}

function unsetup_clusters() {
  # use current-context if pilot_cluster not set
  PILOT_CLUSTER="${PILOT_CLUSTER:-$(kubectl config current-context)}"

  unset IFS
  k_contexts=$(kubectl config get-contexts -o name)
  for context in ${k_contexts}; do
     kubectl config use-context "${context}"

     kubectl delete clusterrolebinding prow-cluster-admin-binding 2>/dev/null
     if [[ "${SETUP_CLUSTERREG}" == "True" && "${PILOT_CLUSTER}" != "$context" ]]; then
        kubectl delete clusterrolebinding istio-multi-test 2>/dev/null
        kubectl delete ns ${SA_NAMESPACE} 2>/dev/null
     fi
  done
  kubectl config use-context "${PILOT_CLUSTER}"
  if [[ "${USE_GKE}" == "True" && "${SETUP_CLUSTERREG}" == "True" ]]; then
     gcloud compute firewall-rules delete istio-multicluster-test-pods --quiet
  fi
}



