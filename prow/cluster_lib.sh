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
NUM_NODES=1
CLUSTER_NAME=
IFS=';' VERSIONS=($(gcloud container get-server-config --project=${PROJECT_NAME} --zone=${ZONE} --format='value(validMasterVersions)'))
CLUSTER_VERSION="${VERSIONS[1]}"
KUBE_USER="istio-prow-test-job@istio-testing.iam.gserviceaccount.com"
CLUSTER_CREATED=false

delete_cluster () {
  if [ "${CLUSTER_CREATED}" = true ]; then
    gcloud container clusters delete ${CLUSTER_NAME}\
      --zone ${ZONE}\
      --project ${PROJECT_NAME}\
      --quiet\
      || echo "Failed to delete cluster ${CLUSTER_NAME}"
  fi
}

function create_cluster() {
  local prefix="${1}"

  CLUSTER_NAME="${prefix}-$(uuidgen | cut -c1-8 | tr "[A-Z]" "[a-z]")"

  echo "Default cluster version: ${CLUSTER_VERSION}"
  gcloud container clusters create ${CLUSTER_NAME}\
    --zone ${ZONE}\
    --project ${PROJECT_NAME}\
    --cluster-version ${CLUSTER_VERSION}\
    --machine-type ${MACHINE_TYPE}\
    --num-nodes ${NUM_NODES}\
    --no-enable-legacy-authorization\
    --enable-kubernetes-alpha --quiet\
    || { echo "Failed to create a new cluster"; exit 1; }
  CLUSTER_CREATED=true

  for i in {1..10}
  do
    status=$(kubectl get namespace || echo "Unreachable")
    [[ ${status} == 'Unreachable' ]] || break
    if [ ${i} -eq 10 ]; then
      echo "Cannot connect to the new cluster"; exit 1
    fi
    sleep 5
  done

  kubectl create clusterrolebinding prow-cluster-admin-binding\
    --clusterrole=cluster-admin\
    --user="${KUBE_USER}"
}
