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

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

PROJECT_NAME=istio-testing
ZONE=us-central1-f
CLUSTER_VERSION=1.7.5
MACHINE_TYPE=n1-standard-4
NUM_NODES=1
CLUSTER_NAME=cluster-wide-auth-$(uuidgen | cut -c1-8 | tr "[A-Z]" "[a-z]")

CLUSTER_CREATED=false

delete_cluster () {
    if [ "${CLUSTER_CREATED}" = true ]; then
        gcloud container clusters delete ${CLUSTER_NAME} --zone ${ZONE} --project ${PROJECT_NAME} --quiet \
          || echo "Failed to delete cluster ${CLUSTER_NAME}"
    fi
}
trap delete_cluster EXIT

if [ -f /home/bootstrap/.kube/config ]; then
  sudo rm /home/bootstrap/.kube/config
fi

gcloud container clusters create ${CLUSTER_NAME} --zone ${ZONE} --project ${PROJECT_NAME} --cluster-version ${CLUSTER_VERSION} \
  --machine-type ${MACHINE_TYPE} --num-nodes ${NUM_NODES} --no-enable-legacy-authorization --enable-kubernetes-alpha --quiet \
  || { echo "Failed to create a new cluster"; exit 1; }
CLUSTER_CREATED=true

kubectl create clusterrolebinding prow-cluster-admin-binding --clusterrole=cluster-admin --user=istio-prow-test-job@istio-testing.iam.gserviceaccount.com

echo 'Running cluster-wide e2e rbac, auth Tests'
./prow/e2e-suite-rbac-auth.sh --cluster_wide true --use_initializer true "${@}"
