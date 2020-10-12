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

readonly SHARED_VPC_HOST_BOSKOS_RESOURCE="shared-vpc-host-gke-project"
readonly SHARED_VPC_SVC_BOSKOS_RESOURCE="shared-vpc-svc-gke-project"

# The network and firewall rule resources are very likely leaked if we
# teardown them with `kubetest2 --down`, so we leverage boskos-janitor here
# since it can make sure the projects can be back to the sanity state.
# TODO(chizhg): find a cleaner and less hacky way to handle this.
function multiproject_multicluster_setup() {
  # Acquire a host project from the project rental pool.
  local host_project
  host_project=$(boskos_acquire "${SHARED_VPC_HOST_BOSKOS_RESOURCE}")
  # Remove all projects that are currently associated with this host project.
  local associated_projects
  associated_projects=$(gcloud beta compute shared-vpc associated-projects list "${host_project}" --format="value(RESOURCE_ID)")
  if [ -n "${associated_projects}" ]; then
    while read -r svc_project
    do
      gcloud beta compute shared-vpc associated-projects remove "${svc_project}" --host-project "${host_project}"
    done <<< "$associated_projects"
  fi

  # Acquire two service projects from the project rental pool.
  local service_project1
  service_project1=$(boskos_acquire "${SHARED_VPC_SVC_BOSKOS_RESOURCE}")
  local service_project2
  service_project2=$(boskos_acquire "${SHARED_VPC_SVC_BOSKOS_RESOURCE}")
  # gcloud requires one service project can only be associated with one host
  # project, so if the acquired service projects have already been associated
  # with one host project, remove the association.
  for service_project in "${service_project1}" "${service_project2}"
  do
    local associated_host_project
    associated_host_project=$(gcloud beta compute shared-vpc get-host-project "${service_project}" --format="value(name)")
    if [ -n "${associated_host_project}" ]; then
      gcloud beta compute shared-vpc associated-projects remove "${service_project}" --host-project "${associated_host_project}"
    fi
  done

  echo "${host_project},${service_project1},${service_project2}"
}

#####################################################################
# Functions used for boskos (the project rental service) operations #
#####################################################################

# Depends on following env vars
# - JOB_NAME: available in all Prow jobs

# Common boskos arguments are presented once.
function boskos_cmd() {
  boskosctl --server-url "http://boskos.test-pods.svc.cluster.local." --owner-name "${JOB_NAME}" "${@}"
}

# Returns a boskos resource name of the given type. Times out in 10 minutes if it cannot get a clean project.
# 1. Lease the resource from boskos.
# 2. Send a heartbeat in the background to keep the lease active.
# Parameters: $1 - resource type. Must be one of the types configured in https://gke-internal.googlesource.com/istio/test-infra-internal/+/refs/heads/master/boskos/config/resources.yaml.
function boskos_acquire() {
  local resource_type="$1"
  local resource
  resource="$( boskos_cmd acquire --type "${resource_type}" --state free --target-state busy --timeout 10m )"
  if [[ -z ${resource} ]]; then
    return 1
  fi

  # Send a heartbeat in the background to keep the lease while using the resource.
  boskos_cmd heartbeat --resource "${resource}" > /dev/null 2> /dev/null &
  jq -r .name <<<"${resource}"
}

# Release a leased boskos resource.
# Parameters: $1 - resource name. Must be the same as returned by the
#                  boskos_acquire function call, e.g. asm-boskos-1.
function boskos_release() {
  local resource_name="$1"
  boskos_cmd release --name "${resource_name}" --target-state dirty
}

#####################################################################
#                    Functions used for ASM                         #
#####################################################################

function print_kubeconfigs() {
  local KUBECONFIGPATHS
  IFS=":" read -r -a KUBECONFIGPATHS <<< "${KUBECONFIG}"
  for i in "${!KUBECONFIGPATHS[@]}"; do
    # View current kubeconfig file for debugging
    kubectl config view --kubeconfig="${KUBECONFIGPATHS[$i]}"
  done
}

# Process kubeconfig files to make sure current-context in each file
# is set correctly.
# Depends on env var ${KUBECONFIG}
function process_kubeconfigs() {
  local KUBECONFIGPATHS
  IFS=":" read -r -a KUBECONFIGPATHS <<< "${KUBECONFIG}"
  # Each kubeconfig file should have one and only one cluster context.
  for i in "${!KUBECONFIGPATHS[@]}"; do
    local CONTEXTSTR
    CONTEXTSTR=$(kubectl config view -o jsonpath="{.contexts[0].name}" --kubeconfig="${KUBECONFIGPATHS[$i]}")
    kubectl config use-context "${CONTEXTSTR}" --kubeconfig="${KUBECONFIGPATHS[$i]}"
  done

  for i in "${!KUBECONFIGPATHS[@]}"; do
    kubectl config view --kubeconfig="${KUBECONFIGPATHS[$i]}"
  done
}

# Prepare images required for the e2e test.
# Depends on env var ${HUB} and ${TAG}
function prepare_images() {
  # Build images from the current branch and push the images to gcr.
  make docker.all HUB="${HUB}" TAG="${TAG}" DOCKER_TARGETS="docker.pilot docker.proxyv2 docker.app"
  docker push "${HUB}/app:${TAG}"
  docker push "${HUB}/pilot:${TAG}"
  docker push "${HUB}/proxyv2:${TAG}"

  docker pull gcr.io/asm-staging-images/asm/stackdriver-prometheus-sidecar:e2e-test
  docker tag gcr.io/asm-staging-images/asm/stackdriver-prometheus-sidecar:e2e-test "${HUB}/stackdriver-prometheus-sidecar:${TAG}"
  docker push "${HUB}/stackdriver-prometheus-sidecar:${TAG}"
}

# Build istioctl in the current branch to install ASM.
function build_istioctl() {
  GO111MODULE=on go get github.com/jteeuwen/go-bindata/go-bindata@6025e8de665b
  PATH="$(go env GOPATH)/bin:$PATH"
  export PATH
  make gen-charts
  make istioctl
  PATH="$PWD/out/linux_amd64:$PATH"
  export PATH
}

# Delete temporary images created for the e2e test.
# Depends on env var ${HUB} and ${TAG}
function cleanup_images() {
  gcloud beta container images delete "${HUB}/app:${TAG}" --force-delete-tags
  gcloud beta container images delete "${HUB}/pilot:${TAG}" --force-delete-tags
  gcloud beta container images delete "${HUB}/proxyv2:${TAG}" --force-delete-tags
  gcloud beta container images delete "${HUB}/stackdriver-prometheus-sidecar:${TAG}" --force-delete-tags
}

# Set permissions to allow test projects to read images for gcr.
# Parameters: $1 - project hosts gcr
#             $2 - array of k8s contexts
function set_permissions() {
  local GCR_PROJECT_ID=$1; shift
  local CONTEXTS=("${@}")

  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    if [[ "${VALS[1]}" != "${GCR_PROJECT_ID}" ]]; then
      PROJECT_NUMBER=$(gcloud projects describe "${VALS[1]}" --format="value(projectNumber)")
      gcloud projects add-iam-policy-binding "${GCR_PROJECT_ID}" \
        --member="serviceAccount:${PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
        --role=roles/storage.objectViewer
    fi
  done
}

# Revert the operations in set_permissions.
# Parameters: $1 - project hosts gcr
#             $2 - array of k8s contexts
function remove_permissions() {
  local GCR_PROJECT_ID="$1"; shift
  local CONTEXTS=("${@}")

  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    if [[ "${VALS[1]}" != "${GCR_PROJECT_ID}" ]]; then
      PROJECT_NUMBER=$(gcloud projects describe "${VALS[1]}" --format="value(projectNumber)")
      gcloud projects remove-iam-policy-binding "${GCR_PROJECT_ID}" \
        --member="serviceAccount:${PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
        --role=roles/storage.objectViewer
    fi
  done
}

# Install ASM on the clusters.
# Parameters: $1 - PKG: Path of Kpt package
#             $2 - CA: CITADEL or MESHCA
#             $3 - array of k8s contexts
# Depends on env var ${HUB} and ${TAG}
function install_asm() {
  local PKG="$1"; shift
  local CA="$1"; shift
  local CONTEXTS=("${@}")

  # Select the ASM profile to use.
  local ASM_PROFILE
  case "$CLUSTER_TOPOLOGY" in
    SINGLE_CLUSTER | MULTICLUSTER)
      ASM_PROFILE="asm-gcp"
      ;;
    MULTIPROJECT_MULTICLUSTER)
      ASM_PROFILE="asm-gcp-multiproject"
      ;;
    *)
      echo "Error: Unable to select profile for cluster topology ${CLUSTER_TOPOLOGY}" >&2
      exit 1
      ;;
  esac

  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    PROJECT_ID="${VALS[1]}"
    LOCATION="${VALS[2]}"
    CLUSTER="${VALS[3]}"
    PROJECT_NUMBER=$(gcloud projects describe "${PROJECT_ID}" --format="value(projectNumber)")

    # Use the first project as the environ project
    if [[ $i == 0 ]]; then
      ENVIRON_PROJECT_NUMBER="${PROJECT_NUMBER}"
      MESH_ID="proj-${ENVIRON_PROJECT_NUMBER}"
      echo "Environ project ID: ${PROJECT_ID}, project number: ${ENVIRON_PROJECT_NUMBER}"
    fi

    if [[ "${CA}" == "MESHCA" ]]; then
      # Configure trust domain aliases for multi-project.
      if [[ "${CLUSTER_TOPOLOGY}" == "MULTIPROJECT_MULTICLUSTER" ]]; then
        # Gather the trust domain aliases from each cluster.
        TRUSTDOMAINALIASES=""
        for j in "${!CONTEXTS[@]}"; do
          if [[ "$i" != "$j" ]]; then
            IFS="_" read -r -a TMP <<< "${CONTEXTS[$j]}"
            if [[ -z "${TRUSTDOMAINALIASES}" ]]; then
              TRUSTDOMAINALIASES="${TMP[1]}.svc.id.goog"
            else
              TRUSTDOMAINALIASES="$TRUSTDOMAINALIASES ${TMP[1]}.svc.id.goog"
            fi
          fi
        done

        kpt cfg set "${PKG}" anthos.servicemesh.trustDomainAliases "$TRUSTDOMAINALIASES"
      fi

      kpt cfg set "${PKG}" \
        anthos.servicemesh.spiffe-bundle-endpoints "${PROJECT_ID}.svc.id.goog|https://storage.googleapis.com/mesh-ca-resources/spiffe_bundle.json"
      kpt cfg set "${PKG}" \
        anthos.servicemesh.token-audiences ""

      yes | istioctl install -f "${PKG}/istio-operator.yaml" --context="${CONTEXTS[$i]}" \
        --set profile="${ASM_PROFILE}" \
        --set hub="${HUB}" \
        --set tag="${TAG}" \
        --set meshConfig.defaultConfig.proxyMetadata.GCP_METADATA="${PROJECT_ID}|${PROJECT_NUMBER}|${CLUSTER}|${LOCATION}" \
        --set meshConfig.defaultConfig.proxyMetadata.TRUST_DOMAIN="${PROJECT_ID}.svc.id.goog" \
        --set meshConfig.defaultConfig.proxyMetadata.GKE_CLUSTER_URL="https://container.googleapis.com/v1/projects/${PROJECT_ID}/locations/${LOCATION}/clusters/${CLUSTER}" \
        --set values.global.meshID="${MESH_ID}" \
        --set values.global.trustDomain="${PROJECT_ID}.svc.id.goog" \
        --set values.global.sds.token.aud="${PROJECT_ID}.svc.id.goog" \
        --set values.global.multiCluster.clusterName="${PROJECT_ID}/${LOCATION}/${CLUSTER}" \
        --set values.telemetry.v2.prometheus.enabled="true"
    elif [[ "${CA}" == "CITADEL" ]]; then
      kubectl create namespace istio-system --context="${CONTEXTS[$i]}"
      kubectl create secret generic cacerts --context="${CONTEXTS[$i]}" \
        -n istio-system \
        --from-file=samples/certs/ca-cert.pem \
        --from-file=samples/certs/ca-key.pem \
        --from-file=samples/certs/root-cert.pem \
        --from-file=samples/certs/cert-chain.pem

      # Configure trust domain aliases for multi-project.
      if [[ "${CLUSTER_TOPOLOGY}" == "MULTIPROJECT_MULTICLUSTER" ]]; then
        kpt cfg set "${PKG}" anthos.servicemesh.trustDomainAliases "${PROJECT_ID}.svc.id.goog"
      fi

      kpt cfg set "${PKG}" anthos.servicemesh.spiffe-bundle-endpoints ""
      kpt cfg set "${PKG}" anthos.servicemesh.token-audiences "istio-ca,${PROJECT_ID}.svc.id.goog"

      yes | istioctl install -f "${PKG}/istio-operator.yaml" --context="${CONTEXTS[$i]}" \
        --set profile="${ASM_PROFILE}" \
        --set hub="${HUB}" \
        --set tag="${TAG}" \
        --set meshConfig.defaultConfig.proxyMetadata.GCP_METADATA="${PROJECT_ID}|${PROJECT_NUMBER}|${CLUSTER}|${LOCATION}" \
        --set meshConfig.defaultConfig.proxyMetadata.TRUST_DOMAIN="cluster.local" \
        --set meshConfig.defaultConfig.proxyMetadata.GKE_CLUSTER_URL="https://container.googleapis.com/v1/projects/${PROJECT_ID}/locations/${LOCATION}/clusters/${CLUSTER}" \
        --set meshConfig.defaultConfig.proxyMetadata.CA_PROVIDER="Citadel" \
        --set meshConfig.defaultConfig.proxyMetadata.PLUGINS="" \
        --set values.global.meshID="${MESH_ID}" \
        --set values.global.caAddress="" \
        --set values.global.pilotCertProvider="istiod" \
        --set values.global.trustDomain="cluster.local" \
        --set values.global.sds.token.aud="${PROJECT_ID}.svc.id.goog" \
        --set values.global.multiCluster.clusterName="${PROJECT_ID}/${LOCATION}/${CLUSTER}" \
        --set values.telemetry.v2.prometheus.enabled="true"
    else
      echo "Invalid CA ${CA}"
      exit 1
    fi
  done

  for i in "${!CONTEXTS[@]}"; do
    for j in "${!CONTEXTS[@]}"; do
      if [[ "$i" != "$j" ]]; then
        IFS="_" read -r -a VALS <<< "${CONTEXTS[$j]}"
        istioctl x create-remote-secret --context="${CONTEXTS[$j]}" --name="${VALS[3]}" | \
          kubectl apply -f - --context="${CONTEXTS[$i]}"
      fi
    done
  done
}