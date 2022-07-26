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

#
# Creates and configures a GKE cluster for running the Istio e2e tests.
# Notes:
# * See README.md
#

PROJECT=${PROJECT:-$(gcloud config list --format 'value(core.project)' 2>/dev/null)}
ZONE=${ZONE:-us-central1-f}
CLUSTER_NAME=${CLUSTER_NAME:-istio-e2e}
MACHINE_TYPE=${MACHINE_TYPE:-n1-standard-4}
NUM_NODES=${NUM_NODES:-3}
# Store the previous value (which may have been unset) so we can restore it on cleanup
OLD_USE_CLIENT_CERT=$(gcloud config list 2>/dev/null | grep use_client_certificate | cut -d' ' -f3)

function usage() {
  echo "${0} -p PROJECT [-z ZONE] [-c CLUSTER_NAME] [-v CLUSTER_VERSION] [-m MACHINE_TYPE] [-n NUM_NODES]"
  echo ''
  # shellcheck disable=SC2016
  echo '  -p: Specifies the GCP Project name. (defaults to $PROJECT_NAME, or current GCP project if unspecified).'
  # shellcheck disable=SC2016
  echo '  -z: Specifies the zone. (defaults to $ZONE, or "us-central1-f").'
  # shellcheck disable=SC2016
  echo '  -c: Specifies the cluster name. (defaults to $CLUSTER_NAME, or "istio-e2e").'
  # shellcheck disable=SC2016
  echo '  -v: Specifies the cluster version. (defaults to $CLUSTER_VERSION, or GCP default if unspecified ).'
  # shellcheck disable=SC2016
  echo '  -m: Specifies the machine type. (defaults to $MACHINE_TYPE, or "n1-standard-4").'
  # shellcheck disable=SC2016
  echo '  -n: Specifies the number of nodes. (defaults to $NUM_NODES, or "3").'
  echo ''
}

# Allow command-line args to override the defaults.
while getopts ":p:z:c:v:m:n:h" opt; do
  case ${opt} in
    p)
      PROJECT=${OPTARG}
      ;;
    z)
      ZONE=${OPTARG}
      ;;
    c)
      CLUSTER_NAME=${OPTARG}
      ;;
    v)
      CLUSTER_VERSION=${OPTARG}
      ;;
    m)
      MACHINE_TYPE=${OPTARG}
      ;;
    n)
      NUM_NODES=${OPTARG}
      ;;
    h)
      usage
      exit 0
      ;;
    \?)
      echo "Invalid option: -$OPTARG" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "${PROJECT}" ]]; then
  echo "Error: PROJECT (-p) must be specified!"
  usage
  exit 1
fi



set -o errexit
set -o nounset
set -o pipefail
set -x # echo on

function cleanup {
  # Reset certificate config.
  if [ -z "$OLD_USE_CLIENT_CERT" ]; then
    gcloud config unset container/use_client_certificate
  else
    gcloud config set container/use_client_certificate "$OLD_USE_CLIENT_CERT"
  fi
}

# Run cleanup before we exit.
trap cleanup EXIT

# Create the cluster
gcloud container clusters create "$CLUSTER_NAME" \
  --project="$PROJECT" \
  --cluster-version="$CLUSTER_VERSION" \
  --zone="$ZONE" \
  --machine-type="$MACHINE_TYPE" \
  --num-nodes="$NUM_NODES" \
  --no-enable-legacy-authorization

# This is a hack to handle the case where clusterrolebinding creation returns:
#
# Error from server (Forbidden): clusterrolebindings.rbac.authorization.k8s.io is forbidden: User "client" cannot
# create clusterrolebindings.rbac.authorization.k8s.io at the cluster scope
gcloud config set container/use_client_certificate False

# Download the credentials for the cluster.
gcloud container clusters get-credentials "$CLUSTER_NAME" --project="$PROJECT" --zone="$ZONE"

# Grant the current user admin privileges on the cluster.
kubectl create clusterrolebinding cluster-admin-binding --clusterrole=cluster-admin --user="$(gcloud config get-value core/account)"
