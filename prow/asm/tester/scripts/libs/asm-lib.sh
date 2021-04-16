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

readonly ROOT_CA_ID_PREFIX="asm-test-root-ca"
readonly ROOT_CA_LOC="us-central1"
readonly SUB_CA_ID_PREFIX="asm-test-sub-ca"

# Process kubeconfig files to make sure current-context in each file
# is set correctly.
# Depends on env var ${KUBECONFIG}
function process_kubeconfigs() {
  local KUBECONFIGPATHS
  IFS=":" read -r -a KUBECONFIGPATHS <<< "${KUBECONFIG}"
  # Each kubeconfig file should have one and only one cluster context.
  for i in "${!KUBECONFIGPATHS[@]}"; do
    local CONTEXT_STR
    CONTEXT_STR=$(kubectl config view -o jsonpath="{.contexts[0].name}" --kubeconfig="${KUBECONFIGPATHS[$i]}")
    kubectl config use-context "${CONTEXT_STR}" --kubeconfig="${KUBECONFIGPATHS[$i]}"
  done

  for i in "${!KUBECONFIGPATHS[@]}"; do
    kubectl config view --kubeconfig="${KUBECONFIGPATHS[$i]}"
  done
}

# Prepare images required for the e2e test.
# Depends on env var ${HUB} and ${TAG}
function prepare_images() {
  # Configure Docker to authenticate with Container Registry.
  gcloud auth configure-docker
  # Build images from the current branch and push the images to gcr.
  make docker.all HUB="${HUB}" TAG="${TAG}" DOCKER_TARGETS="docker.pilot docker.proxyv2 docker.app"
  docker push "${HUB}/app:${TAG}"
  docker push "${HUB}/pilot:${TAG}"
  docker push "${HUB}/proxyv2:${TAG}"

  docker pull gcr.io/asm-staging-images/asm/stackdriver-prometheus-sidecar:e2e-test
  docker tag gcr.io/asm-staging-images/asm/stackdriver-prometheus-sidecar:e2e-test "${HUB}/stackdriver-prometheus-sidecar:${TAG}"
  docker push "${HUB}/stackdriver-prometheus-sidecar:${TAG}"
}

# Prepare images (istiod, proxyv2) required for the managed control plane e2e test.
# Depends on env var ${HUB} and ${TAG}
function prepare_images_for_managed_control_plane() {
  # Configure Docker to authenticate with Container Registry.
  gcloud auth configure-docker
  # Build images from the current branch and push the images to gcr.
  HUB="${HUB}" TAG="${TAG}" make push.docker.cloudrun push.docker.proxyv2 push.docker.app
}

# Build istioctl in the current branch to install ASM.
function build_istioctl() {
  make istioctl
  cp "$PWD/out/linux_amd64/istioctl" "/usr/local/bin"
}

# Delete temporary images created for the e2e test.
# Depends on env var ${HUB} and ${TAG}
function cleanup_images() {
  gcloud beta container images delete "${HUB}/app:${TAG}" --force-delete-tags --quiet
  gcloud beta container images delete "${HUB}/pilot:${TAG}" --force-delete-tags --quiet
  gcloud beta container images delete "${HUB}/proxyv2:${TAG}" --force-delete-tags --quiet
  gcloud beta container images delete "${HUB}/stackdriver-prometheus-sidecar:${TAG}" --force-delete-tags --quiet
}

# Unset the exported HTTP_PROXY and HTTPS_PROXY in Baremetal Installation
function unset_http_proxy() {
  unset HTTP_PROXY
  unset HTTPS_PROXY
}

# Delete temporary images created for the managed control plane e2e test.
# Depends on env var ${HUB} and ${TAG}
function cleanup_images_for_managed_control_plane() {
  gcloud beta container images delete "${HUB}/cloudrun:${TAG}" --force-delete-tags || true
  gcloud beta container images delete "${HUB}/proxyv2:${TAG}" --force-delete-tags || true
}

# Set permissions to allow test projects to read images for gcr for testing with
# GCP.
# Parameters: $1 - project hosts gcr
#             $2 - a string of k8s contexts
function set_gcp_permissions() {
  local GCR_PROJECT_ID=$1
  IFS="," read -r -a CONTEXTS <<< "$2"

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

# Revert the operations in set_gcp_permissions.
# Parameters: $1 - project hosts gcr
#             $2 - a string of k8s contexts
function remove_gcp_permissions() {
  local GCR_PROJECT_ID="$1"
  IFS="," read -r -a CONTEXTS <<< "$2"

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

# Register the clusters into the Hub of the Hub host project.
# Parameters: $1 - Hub host project
#             $2 - array of k8s contexts
function register_clusters_in_hub() {
  local GKEHUB_PROJECT_ID=$1; shift
  local CONTEXTS=("${@}")
  local ENVIRON_PROJECT_NUMBER
  ENVIRON_PROJECT_NUMBER=$(gcloud projects describe "${GKEHUB_PROJECT_ID}" --format="value(projectNumber)")

  # Create Hub service account for the Hub host project
  gcloud beta services identity create --service=gkehub.googleapis.com --project="${GKEHUB_PROJECT_ID}"

  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    echo "Hub registration for ${CONTEXTS[$i]}"
    local PROJECT_ID=${VALS[1]}
    local CLUSTER_LOCATION=${VALS[2]}
    local CLUSTER_NAME=${VALS[3]}
    # Add IAM binding for Hub SA in the Hub connect project
    gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
      --member "serviceAccount:service-${ENVIRON_PROJECT_NUMBER}@gcp-sa-gkehub.iam.gserviceaccount.com" \
      --role roles/gkehub.serviceAgent
    # This is the user guide for registering clusters within Hub
    # https://cloud.devsite.corp.google.com/service-mesh/docs/register-cluster
    # Verify two ways of Hub registration
    # if the cluster is in the Hub host project
    if [[ "${PROJECT_ID}" == "${GKEHUB_PROJECT_ID}" ]]; then
      gcloud beta container hub memberships register "${CLUSTER_NAME}" --project="${PROJECT_ID}" \
        --gke-cluster="${CLUSTER_LOCATION}"/"${CLUSTER_NAME}" \
        --enable-workload-identity \
        --quiet
    # if the cluster is in the connect project
    else
      gcloud beta container hub memberships register "${CLUSTER_NAME}" --project="${GKEHUB_PROJECT_ID}" \
        --gke-uri=https://container.googleapis.com/v1/projects/"${PROJECT_ID}"/locations/"${CLUSTER_LOCATION}"/clusters/"${CLUSTER_NAME}" \
        --enable-workload-identity \
        --quiet
    fi
  done
  echo "These are the Hub Memberships within Host project ${GKEHUB_PROJECT_ID}"
  gcloud beta container hub memberships list --project="${GKEHUB_PROJECT_ID}"
}

# Revert the operations in register_clusters_in_hub.
# Parameters: $1 - Hub host project
#             $2 - a string of k8s contexts
function cleanup_hub_setup() {
  local GKEHUB_PROJECT_ID=$1
  IFS="," read -r -a CONTEXTS <<< "$2"

  local ENVIRON_PROJECT_NUMBER
  ENVIRON_PROJECT_NUMBER=$(gcloud projects describe "${GKEHUB_PROJECT_ID}" --format="value(projectNumber)")
  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    local PROJECT_ID=${VALS[1]}
    local CLUSTER_NAME=${VALS[3]}
    # Remove added IAM binding for Hub SA
    if [[ "${PROJECT_ID}" != "${GKEHUB_PROJECT_ID}" ]]; then
      gcloud projects remove-iam-policy-binding "${PROJECT_ID}" \
        --member "serviceAccount:service-${ENVIRON_PROJECT_NUMBER}@gcp-sa-gkehub.iam.gserviceaccount.com" \
        --role roles/gkehub.serviceAgent
    fi
  done
}

# Set permissions to allow test projects to read images for gcr for testing with
# multicloud.
# Parameters: $1 - project hosts gcr
function set_multicloud_permissions() {
  local GCR_PROJECT_ID="$1"
  local CONFIGS=( "${MC_CONFIGS[@]}" )
  if [[ "${CLUSTER_TYPE}" == "bare-metal" ]]; then
    export HTTP_PROXY
  fi
  local SECRETNAME="test-gcr-secret"
  for i in "${!CONFIGS[@]}"; do
    kubectl create ns istio-system --kubeconfig="${CONFIGS[$i]}"

    kubectl create secret -n istio-system docker-registry "${SECRETNAME}" \
      --docker-server=https://gcr.io \
      --docker-username=_json_key \
      --docker-email="$(gcloud config get-value account)" \
      --docker-password="$(cat "${GOOGLE_APPLICATION_CREDENTIALS}")" \
      --kubeconfig="${CONFIGS[$i]}"

    # Save secret data once, to be passed into the test framework
    if [[ "${i}" == 0 ]]; then
      export TEST_IMAGE_PULL_SECRET="${ARTIFACTS}/test_image_pull_secret.yaml"
      kubectl -n istio-system get secrets test-gcr-secret \
        --kubeconfig="${CONFIGS[$i]}" \
        -o yaml \
      | sed '/namespace/d' > "${TEST_IMAGE_PULL_SECRET}"
    fi

    while read -r SA; do
      add_image_pull_secret_to_sa "${SA}" "${SECRETNAME}" "${CONFIGS[$i]}"
    done <<EOF
default
istio-ingressgateway-service-account
istio-reader-service-account
istiod-service-account
EOF

  done
  if [[ "${CLUSTER_TYPE}" == "bare-metal" ]]; then
    export -n HTTP_PROXY
  fi
}

# Add the imagePullSecret to the service account
# Parameters: $1 - service account
#             $2 - secret name
#             $3 - kubeconfig
function add_image_pull_secret_to_sa() {
  cat <<EOF | kubectl --kubeconfig="${3}" apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${1}
  namespace: istio-system
imagePullSecrets:
- name: ${2}
EOF
}

# Setup the private CAs.
# Parameters: $1 - a string of k8s contexts
function setup_private_ca() {
  IFS="," read -r -a CONTEXTS <<< "$1"

  local ROOT_CA_ID="${ROOT_CA_ID_PREFIX}-${BUILD_ID}"
  # Create root CA in the central project if it does not exist.
  if ! gcloud beta privateca roots list --project "${SHARED_GCP_PROJECT}" --location "${ROOT_CA_LOC}" | grep -q "${ROOT_CA_ID}"; then
    echo "Creating root CA ${ROOT_CA_ID}..."
    gcloud beta privateca roots create "${ROOT_CA_ID}" \
      --location "${ROOT_CA_LOC}" \
      --project "${SHARED_GCP_PROJECT}" \
      --subject "CN=ASM Test Root CA, O=Google LLC" \
      --tier enterprise \
      --reusable-config root-unconstrained \
      --key-algorithm rsa-pkcs1-4096-sha256
  fi

  # Contains temporary files for subordinate CA activation.
  WORKING_DIR=$(mktemp -d)
  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    PROJECT_ID="${VALS[1]}"
    LOCATION="${VALS[2]}"
    CLUSTER="${VALS[3]}"
    local SUBORDINATE_CA_ID="${SUB_CA_ID_PREFIX}-${BUILD_ID}-${CLUSTER}"
    local CSR_FILE="${WORKING_DIR}/${SUBORDINATE_CA_ID}.csr"
    local CERT_FILE="${WORKING_DIR}/${SUBORDINATE_CA_ID}.crt"
    if ! gcloud beta privateca subordinates list --location "${LOCATION}" --project "${PROJECT_ID}" | grep -q "${SUBORDINATE_CA_ID}"; then
      echo "Creating subordinate CA ${SUBORDINATE_CA_ID}..."
      gcloud beta privateca subordinates create "${SUBORDINATE_CA_ID}" \
        --location "${LOCATION}" \
        --project "${PROJECT_ID}" \
        --subject "CN=ASM Test Subordinate CA, O=Google LLC" \
        --tier devops \
        --reusable-config "subordinate-mtls-pathlen-0" \
        --key-algorithm rsa-pkcs1-2048-sha256 \
        --create-csr \
        --csr-output-file "${CSR_FILE}"

      CERT_ID="cert-${SUBORDINATE_CA_ID}"
      echo "Signing subordinate CA certificate ${CERT_ID}.."
      gcloud beta privateca certificates create "${CERT_ID}" \
        --issuer "${ROOT_CA_ID}" \
        --issuer-location "${ROOT_CA_LOC}" \
        --project "${SHARED_GCP_PROJECT}" \
        --csr "${CSR_FILE}" \
        --cert-output-file "${CERT_FILE}" \
        --validity "P3Y" # Change this as needed - 3Y is the default for subordinate CAs.

      echo "Activating subordinate CA.."
      gcloud beta privateca subordinates activate "${SUBORDINATE_CA_ID}" \
        --location "${LOCATION}" \
        --project "${PROJECT_ID}" \
        --pem-chain "${CERT_FILE}"
    fi
  done
}

# Cleanup the private CAs.
# Parameters: $1 - a string of k8s contexts
function cleanup_private_ca() {
  # Install the uuid tool to generate uuid for the curl command to purge CA in
  # the end.
  apt-get update
  apt-get install uuid -y

  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    PROJECT_ID="${VALS[1]}"
    LOCATION="${VALS[2]}"
    CLUSTER="${VALS[3]}"

    local SUBORDINATE_CA_ID="${SUB_CA_ID_PREFIX}-${BUILD_ID}-${CLUSTER}"
    if gcloud beta privateca subordinates list --project "${PROJECT_ID}" --location "${LOCATION}" | grep -q "${SUBORDINATE_CA_ID}"; then
      echo "Purging subordinate CA $SUBORDINATE_CA_ID.."
      purge-ca "subordinates" "${SUBORDINATE_CA_ID}" "${LOCATION}" "${PROJECT_ID}"
    fi
  done
  local ROOT_CA_ID="${ROOT_CA_ID_PREFIX}-${BUILD_ID}"
  if gcloud beta privateca roots list --project "${SHARED_GCP_PROJECT}" --location "${ROOT_CA_LOC}" | grep -q "${ROOT_CA_ID}"; then
    echo "Purging root CA $ROOT_CA_ID.."
    purge-ca "roots" "${ROOT_CA_ID}" "${ROOT_CA_LOC}" "${SHARED_GCP_PROJECT}"
  fi
}

# Purge the CA.
# Parameters: $1 - the command group (roots|subordinates)
#             $2 - the full CA resource name
#             $3 - location
#             $4 - project ID
function purge-ca() {
  echo "Purging CA '$2'."
  local certs
  # shellcheck disable=SC2207
  certs=($(gcloud beta privateca certificates list \
    --issuer "$2" \
    --format 'table(name.scope(), revocation_details)' \
    --location="$3" --project="$4" | grep -Po '([^\s]+)(?=\s+ACTIVE)' || true))

  echo "> Revoking ${#certs[@]} certificates.." >&2

  for cert in "${certs[@]}"; do
    gcloud beta privateca certificates revoke --certificate "$cert" --quiet
  done

  if gcloud beta privateca "$1" describe "$2" --format "value(state)" --location="$3" --project="$4" | grep -q "ENABLED" ; then
    echo "> Disabling and deleting CA.."
    gcloud beta privateca "$1" disable "$2" --location="$3" --project="$4" --quiet
    # As suggested in
    # https://buganizer.corp.google.com/issues/179162450#comment10, delete root
    # CA with the curl command instead of calling `gcloud beta privateca roots
    # delete`
    if [[ "$4" == "${SHARED_GCP_PROJECT}" ]]; then
      curl \
        -H "Authorization: Bearer $(gcloud auth print-access-token)" \
        -H "Content-Type: application/json" \
        -X POST \
        -d '{ "deletePendingDuration": "0s" }' \
        "https://privateca.googleapis.com/v1beta1/projects/$4/locations/$3/certificateAuthorities/$2:scheduleDelete?requestId=$(uuid)"
    else
      gcloud beta privateca "$1" delete "$2" --location="$3" --project="$4" --quiet
    fi
  fi
}

# Install ASM on the clusters.
# Parameters: $1 - PKG: Path of Kpt package
#             $2 - CA: CITADEL, MESHCA or PRIVATECA
#             $3 - WIP: GKE or HUB
#             $4 - OVERLAY: custom install options
#             $5 - CUSTOM_REVISION_FLAGS: per-revision scriptaro options
#             $6 - array of k8s contexts
# Depends on env var ${HUB} and ${TAG}
function install_asm() {
  local PKG="$1"; shift
  local CA="$1"; shift
  local WIP="$1"; shift
  local OVERLAY="$1"; shift
  local CUSTOM_REVISION_FLAGS="$1"; shift
  local REVISION="$1"; shift
  local CONTEXTS=("${@}")

  if [ -n "$REVISION" ]; then
    CUSTOM_REVISION_FLAGS+=" --revision_name ${REVISION}"
  fi

  # variables that should not persist across install_asm calls should be added here
  local CUSTOM_CA_FLAGS

  # Setup NAT to grant private nodes outbound internet access
  if [[ "${FEATURE_TO_TEST}" == "VPC_SC" ]]; then
    local NETWORK_NAME="default"
    if [[ "${CLUSTER_TOPOLOGY}" == "mp" ]]; then
      NETWORK_NAME="test-network"
    fi
    gcloud compute routers create test-router \
      --network "${NETWORK_NAME}" \
      --region us-central1 || echo "test-router already exists"
    gcloud compute routers nats create test-nat \
      --router=test-router \
      --auto-allocate-nat-external-ips \
      --nat-all-subnet-ip-ranges \
      --router-region=us-central1 \
      --enable-logging || echo "test-nat already exists"
  fi

  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    PROJECT_ID="${VALS[1]}"
    LOCATION="${VALS[2]}"
    CLUSTER="${VALS[3]}"

    CUSTOM_OVERLAY="${PKG}/overlay/default.yaml"
    if [ -n "${OVERLAY}" ]; then
      CUSTOM_OVERLAY="${CUSTOM_OVERLAY},${PKG}/${OVERLAY}"
    fi

    # Use the first project as the environ project
    if [[ $i == 0 ]]; then
      ENVIRON_PROJECT_NUMBER=$(gcloud projects describe "${PROJECT_ID}" --format="value(projectNumber)")
      echo "Environ project ID: ${PROJECT_ID}, project number: ${ENVIRON_PROJECT_NUMBER}"
    fi

    # Create the istio-system ns before running the install_asm script.
    # TODO(chizhg): remove this line after install_asm script can create it.
    kubectl create namespace istio-system --dry-run=client -o yaml | kubectl apply -f - --context="${CONTEXTS[$i]}"
    if [[ "${CA}" == "MESHCA" || "${CA}" == "PRIVATECA" ]]; then
      INSTALL_ASM_CA="mesh_ca"
      if [[ "${CLUSTER_TOPOLOGY}" == "mp"  ]]; then
        TRUSTED_GCP_PROJECTS=""
        for j in "${!CONTEXTS[@]}"; do
          if [[ "$i" != "$j" ]]; then
            IFS="_" read -r -a TMP <<< "${CONTEXTS[$j]}"
            TRUSTED_GCP_PROJECTS="${TMP[1]},$TRUSTED_GCP_PROJECTS"
          fi
        done
      fi

      # b/177358640: for Prow jobs running with GKE staging/staging2 clusters, overwrite
      # GKE_CLUSTER_URL with a custom overlay to fix the issue in installing ASM
      # with MeshCA.
      if [[ "${CLOUDSDK_API_ENDPOINT_OVERRIDES_CONTAINER}" == "https://staging-container.sandbox.googleapis.com/" ]]; then
        kpt cfg set "${PKG}" gcloud.core.project "${PROJECT_ID}"
        kpt cfg set "${PKG}" gcloud.compute.location "${LOCATION}"
        kpt cfg set "${PKG}" gcloud.container.cluster "${CLUSTER}"
        CUSTOM_OVERLAY="${CUSTOM_OVERLAY},${PKG}/overlay/meshca-staging-gke.yaml"
      fi
      if [[ "${CLOUDSDK_API_ENDPOINT_OVERRIDES_CONTAINER}" == "https://staging2-container.sandbox.googleapis.com/" ]]; then
        kpt cfg set "${PKG}" gcloud.core.project "${PROJECT_ID}"
        kpt cfg set "${PKG}" gcloud.compute.location "${LOCATION}"
        kpt cfg set "${PKG}" gcloud.container.cluster "${CLUSTER}"
        CUSTOM_OVERLAY="${CUSTOM_OVERLAY},${PKG}/overlay/meshca-staging2-gke.yaml"
      fi

      # Temporary. Private CA will get its own installation profile in the asm install scripts
      if [[ "${CA}" == "PRIVATECA" ]]; then
        local WORKLOAD_IDENTITY="$PROJECT_ID.svc.id.goog[istio-system/istiod-service-account]"
        local SUBORDINATE_CA_ID="${SUB_CA_ID_PREFIX}-${BUILD_ID}-${CLUSTER}"
        local CA_NAME="projects/${PROJECT_ID}/locations/${LOCATION}/certificateAuthorities/${SUBORDINATE_CA_ID}"
        kpt cfg set "${PKG}" anthos.servicemesh.external_ca.ca_name "${CA_NAME}"
        kpt cfg set "${PKG}" gcloud.core.project "${PROJECT_ID}"
        if [[ "${WIP}" != "HUB" ]]; then
          CUSTOM_OVERLAY="${CUSTOM_OVERLAY},${PKG}/overlay/private-ca.yaml"
        fi
        gcloud beta privateca subordinates add-iam-policy-binding "${SUBORDINATE_CA_ID}" \
          --location "${LOCATION}" \
          --project "${PROJECT_ID}" \
          --member "serviceAccount:$WORKLOAD_IDENTITY" \
          --role "roles/privateca.certificateManager" \
          --quiet
      fi
    elif [[ "${CA}" == "CITADEL" ]]; then
      INSTALL_ASM_CA="citadel"
      CUSTOM_CA_FLAGS="--ca_cert samples/certs/ca-cert.pem \
--ca_key samples/certs/ca-key.pem \
--root_cert samples/certs/root-cert.pem \
--cert_chain samples/certs/cert-chain.pem"
    else
      echo "Invalid CA ${CA}"
      exit 1
    fi

    if [[ "${FEATURE_TO_TEST}" == "VPC_SC" ]]; then
      # Set up the firewall for VPC-SC
      FIREWALL_RULE_NAME=$(gcloud compute firewall-rules list --filter="name~gke-""${CLUSTER}""-[0-9a-z]*-master" --format=json | jq -r '.[0].name')
      gcloud compute firewall-rules update "${FIREWALL_RULE_NAME}" --allow tcp --source-ranges 0.0.0.0/0
      # Check the updated firewall rule
      gcloud compute firewall-rules list --filter="name~gke-""${CLUSTER}""-[0-9a-z]*-master"
    elif [[ "${FEATURE_TO_TEST}" == "USER_AUTH" ]]; then
      # Add user auth overlay
      CUSTOM_OVERLAY="${CUSTOM_OVERLAY},${PKG}/overlay/user-auth.yaml"
    fi

    # INSTALL_ASM_BRANCH is one of the branches in https://github.com/GoogleCloudPlatform/anthos-service-mesh-packages
    INSTALL_ASM_BRANCH="master"
    curl -O https://raw.githubusercontent.com/GoogleCloudPlatform/anthos-service-mesh-packages/"${INSTALL_ASM_BRANCH}"/scripts/asm-installer/install_asm
    TRUSTED_GCP_PROJECTS="${TRUSTED_GCP_PROJECTS:=}"
    CUSTOM_CA_FLAGS="${CUSTOM_CA_FLAGS:=}"
    chmod +x install_asm

    # Env variable for CI options on install_asm
    export _CI_ISTIOCTL_REL_PATH="$PWD/out/linux_amd64/istioctl"
    export _CI_ASM_IMAGE_LOCATION="${HUB}"
    export _CI_ASM_IMAGE_TAG="${TAG}"
    export _CI_ASM_KPT_BRANCH="${INSTALL_ASM_BRANCH}"
    export _CI_NO_VALIDATE=1

    if [ -n "$REVISION" ]; then
      export _CI_NO_REVISION=0
    else
      export _CI_NO_REVISION=1
    fi

    # Required when using unreleased Scriptaro
    export _CI_ASM_PKG_LOCATION="asm-staging-images"

    if [[ "${WIP}" != "HUB" ]]; then
      if [[ "${CLUSTER_TOPOLOGY}" == "MULTIPROJECT_MULTICLUSTER" || "${CLUSTER_TOPOLOGY}" == "mp" ]]; then
        export _CI_ENVIRON_PROJECT_NUMBER="${ENVIRON_PROJECT_NUMBER}"
        export _CI_TRUSTED_GCP_PROJECTS="${TRUSTED_GCP_PROJECTS}"
        eval ./install_asm \
          --project_id "${PROJECT_ID}" \
          --cluster_name "${CLUSTER}" \
          --cluster_location "${LOCATION}" \
          --ca ${INSTALL_ASM_CA} \
          "${CUSTOM_CA_FLAGS}" \
          --mode install \
          --enable_all \
          --option multiproject \
          --option audit-authorizationpolicy \
          --custom_overlay "${CUSTOM_OVERLAY}" \
          "${CUSTOM_REVISION_FLAGS}" \
          --verbose
      else
        eval ./install_asm \
          --project_id "${PROJECT_ID}" \
          --cluster_name "${CLUSTER}" \
          --cluster_location "${LOCATION}" \
          --ca ${INSTALL_ASM_CA} \
          "${CUSTOM_CA_FLAGS}" \
          --mode install \
          --enable_all \
          --custom_overlay "${CUSTOM_OVERLAY}" \
          --option audit-authorizationpolicy \
          "${CUSTOM_REVISION_FLAGS}" \
          --verbose
      fi
    else
      if [[ "${CLUSTER_TOPOLOGY}" == "MULTIPROJECT_MULTICLUSTER" || "${CLUSTER_TOPOLOGY}" == "mp" ]]; then
        export _CI_ENVIRON_PROJECT_NUMBER="${ENVIRON_PROJECT_NUMBER}"
        export _CI_TRUSTED_GCP_PROJECTS="${TRUSTED_GCP_PROJECTS}"
        eval ./install_asm \
          --project_id "${PROJECT_ID}" \
          --cluster_name "${CLUSTER}" \
          --cluster_location "${LOCATION}" \
          --ca ${INSTALL_ASM_CA} \
          "${CUSTOM_CA_FLAGS}" \
          --mode install \
          --enable_all \
          --option multiproject \
          --option hub-meshca \
          --custom_overlay "${CUSTOM_OVERLAY}" \
          --option audit-authorizationpolicy \
          "${CUSTOM_REVISION_FLAGS}" \
          --verbose
      else
        if [[ "${USE_VM}" == false ]]; then
          eval ./install_asm \
            --project_id "${PROJECT_ID}" \
            --cluster_name "${CLUSTER}" \
            --cluster_location "${LOCATION}" \
            --ca ${INSTALL_ASM_CA} \
            "${CUSTOM_CA_FLAGS}" \
            --mode install \
            --enable_all \
            --option hub-meshca \
            --custom_overlay "${CUSTOM_OVERLAY}" \
            --option audit-authorizationpolicy \
            "${CUSTOM_REVISION_FLAGS}" \
            --verbose
        else
          eval ./install_asm \
            --project_id "${PROJECT_ID}" \
            --cluster_name "${CLUSTER}" \
            --cluster_location "${LOCATION}" \
            --ca ${INSTALL_ASM_CA} \
            "${CUSTOM_CA_FLAGS}" \
            --mode install \
            --enable_all \
            --option hub-meshca \
            --option vm \
            --custom_overlay "${CUSTOM_OVERLAY}" \
            --option audit-authorizationpolicy \
            "${CUSTOM_REVISION_FLAGS}" \
            --verbose
        fi
      fi
    fi
  done

  for i in "${!CONTEXTS[@]}"; do
    for j in "${!CONTEXTS[@]}"; do
      if [[ "$i" != "$j" ]]; then
        IFS="_" read -r -a VALS <<< "${CONTEXTS[$j]}"
        local PROJECT_J=${VALS[1]}
        local LOCATION_J=${VALS[2]}
        local CLUSTER_J=${VALS[3]}
        istioctl x create-remote-secret --context="${CONTEXTS[$j]}" --name="${CLUSTER_J}" > "${PROJECT_J}"_"${LOCATION_J}"_"${CLUSTER_J}".secret
        if [[ "${FEATURE_TO_TEST}" == "VPC_SC" ]]; then
          # For private clusters, convert the cluster masters' public IPs to private IPs.
          PRIV_IP=$(gcloud container clusters describe "${CLUSTER_J}" --project "${PROJECT_J}" --zone "${LOCATION_J}" --format "value(privateClusterConfig.privateEndpoint)")
          sed -i 's/server\:.*/server\: https:\/\/'"${PRIV_IP}"'/' "${PROJECT_J}"_"${LOCATION_J}"_"${CLUSTER_J}".secret
        fi
        kubectl apply -f "${PROJECT_J}"_"${LOCATION_J}"_"${CLUSTER_J}".secret --context="${CONTEXTS[$i]}"
        if [ "${USE_VM}" == true ]; then
          # TODO this is temporary until we have a user-facing way to enable multi-cluster + VMs
          kubectl -n istio-system set env deployment/istiod --context="${CONTEXTS[$i]}" PILOT_ENABLE_CROSS_CLUSTER_WORKLOAD_ENTRY=true
          # we have to wait for new pods, but we do this later so the pods can come up in parallel per-cluster
        fi
      fi
    done
  done

  # TODO this is temporary until we have a user-facing way to enable multi-cluster + VMs
  # we have to wait for all the istiod pods to be updated, asm_vm doens't like it when istiods are dropped
  for i in "${!CONTEXTS[@]}"; do
    WAITING_FOR_ISTIOD=true
    while "${WAITING_FOR_ISTIOD}" && "${USE_VM}"; do
      PODS="istiod-pods-$i.json"
      kubectl --context "${CONTEXTS[$i]}" -n istio-system get po -l app=istiod -ojson > "${PODS}"
      N_PODS="$(jq '.items | length' < "${PODS}")"
      READY_PODS="$(jq '.items[].spec.containers[].env[] | select(.name == "PILOT_ENABLE_CROSS_CLUSTER_WORKLOAD_ENTRY") | .value' < "${PODS}"| wc -l | xargs)"
      if [[ "$N_PODS" == "$READY_PODS" ]]; then
        WAITING_FOR_ISTIOD=false
      fi
    done
  done
}

# Install ASM managed control plane.
# Parameters: $1 - array of k8s contexts
# Depends on env var ${HUB} and ${TAG}
function install_asm_managed_control_plane() {
  # For installation, we'll use the corresponding master branch Scriptaro for ASM branch master-asm.
  git clone --branch master https://github.com/GoogleCloudPlatform/anthos-service-mesh-packages.git
  cp anthos-service-mesh-packages/scripts/asm-installer/install_asm .
  # ASM MCP Prow job connects to staging meshconfig API.
  # The reason is currently, we also use this job to test/alert our staging ADS proxy.
  sed -i 's/meshconfig\.googleapis\.com/staging-meshconfig.sandbox.googleapis.com/g' install_asm
  chmod +x install_asm

  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    local PROJECT_ID="${VALS[1]}"
    local LOCATION="${VALS[2]}"
    local CLUSTER_NAME="${VALS[3]}"
    # Use the first project as the environ project
    if [[ $i == 0 ]]; then
      ENVIRON_PROJECT_NUMBER=$(gcloud projects describe "${PROJECT_ID}" --format="value(projectNumber)")
      echo "Environ project ID: ${PROJECT_ID}, project number: ${ENVIRON_PROJECT_NUMBER}"
    fi
    TMPDIR=$(mktemp -d)

    # _CI_ASM_PKG_LOCATION _CI_ASM_IMAGE_LOCATION are required for unreleased Scriptaro (master and staging branch).
    # Managed control plane installation will use _CI_ASM_PKG_LOCATION, _CI_ASM_IMAGE_LOCATION for Gateway.
    # For sidecar proxy and Istiod, _CI_CLOUDRUN_IMAGE_HUB and _CI_CLOUDRUN_IMAGE_TAG are used.
    # Currently, we only test with mesh CA.
    _CI_ASM_PKG_LOCATION="asm-staging-images" _CI_ASM_IMAGE_LOCATION="${HUB}" _CI_ASM_IMAGE_TAG="${TAG}" _CI_ASM_KPT_BRANCH="master" _CI_CLOUDRUN_IMAGE_HUB="${HUB}/cloudrun" _CI_CLOUDRUN_IMAGE_TAG="${TAG}" "./install_asm" \
      --mode install \
      --project_id "${PROJECT_ID}" \
      --cluster_location "${LOCATION}" \
      --cluster_name "${CLUSTER_NAME}" \
      --managed \
      --enable_cluster_labels \
      --output_dir "${TMPDIR}" \
      --ca "mesh_ca" \
      --verbose
    kubectl apply -f tools/packaging/knative/gateway/injected-gateway.yaml -n istio-system --context="${CONTEXTS[$i]}"
    # Enable access logging to help with debugging tests
    cat <<EOF | kubectl apply --context="${CONTEXTS[$i]}" -f -
apiVersion: v1
data:
  mesh: |-
    accessLogFile: /dev/stdout
kind: ConfigMap
metadata:
  name: asm
  namespace: istio-system
EOF
  done

  for i in "${!CONTEXTS[@]}"; do
    for j in "${!CONTEXTS[@]}"; do
      if [[ "$i" != "$j" ]]; then
        IFS="_" read -r -a VALS <<< "${CONTEXTS[$j]}"
        local PROJECT_J=${VALS[1]}
        local LOCATION_J=${VALS[2]}
        local CLUSTER_J=${VALS[3]}
        # TODO(ruigu): Ideally, the istioctl here should be the same what Scriptaro used above.
        # However, we don't have a clear way of figuring that out.
        # For now, similar to ASM unmanaged, use the locally built istioctl.
        istioctl x create-remote-secret --context="${CONTEXTS[$j]}" --name="${CLUSTER_J}" > "${PROJECT_J}"_"${LOCATION_J}"_"${CLUSTER_J}".secret
        kubectl apply -f "${PROJECT_J}"_"${LOCATION_J}"_"${CLUSTER_J}".secret --context="${CONTEXTS[$i]}"
      fi
    done
  done
}


# Outputs YAML to the given file, in the structure of []cluster.Config to inform the test framework of details about
# each cluster under test. cluster.Config is defined in pkg/test/framework/components/cluster/factory.go.
# Parameters: $1 - path to the file to append to
#             $2 - array of k8s contexts
# Depends on env var ${KUBECONFIG}
function gen_topology_file() {
    local file="$1"; shift
    local contexts=("${@}")

    local kubeconfpaths
    IFS=":" read -r -a kubeconfpaths <<< "${KUBECONFIG}"

    for i in "${!contexts[@]}"; do
      IFS="_" read -r -a VALS <<< "${contexts[$i]}"
      local PROJECT_ID=${VALS[1]}
      local CLUSTER_LOCATION=${VALS[2]}
      local CLUSTER_NAME=${VALS[3]}
      cat <<EOF >> "${file}"
- clusterName: "cn-${PROJECT_ID}-${CLUSTER_LOCATION}-${CLUSTER_NAME}"
  kind: Kubernetes
  meta:
    kubeconfig: "${kubeconfpaths[i]}"
EOF
      # Disable using simulated Pod-based "VMs" when testing real VMs
      if [ -n "${STATIC_VMS}" ] || "${GCE_VMS}"; then
        echo '    fakeVM: false' >> "${file}"
      fi
    done
}

# Install ASM on the clusters.
# Parameters: $1 - CA: CITADEL, MESHCA or PRIVATECA
#             $2 - WIP: GKE or HUB
#             $3 - array of k8s contexts
# Depends on env var ${HUB} and ${TAG}
# TODO(gzip) remove this function once b/176177944 is fixed
function install_asm_on_multicloud() {
  local CA="$1"; shift
  local WIP="$1"; shift
  local MESH_ID="test-mesh"

  export HERCULES_CLI_LAB="atl_shared"
  USER=${USER:-prowuser}
  export USER

  for i in "${!MC_CONFIGS[@]}"; do
    install_certs "${MC_CONFIGS[$i]}"

    echo "----------Installing ASM----------"
    if [[ "${WIP}" == "HUB" ]]; then
      local IDENTITY_PROVIDER
      local IDENTITY
      local HUB_MEMBERSHIP_ID
      IDENTITY_PROVIDER="$(kubectl --kubeconfig="${MC_CONFIGS[$i]}" --context=cluster get memberships membership -o=json | jq .spec.identity_provider)"
      IDENTITY="$(echo "${IDENTITY_PROVIDER}" | sed 's/^\"https:\/\/gkehub.googleapis.com\/projects\/\(.*\)\/locations\/global\/memberships\/\(.*\)\"$/\1 \2/g')"
      read -r ENVIRON_PROJECT_ID HUB_MEMBERSHIP_ID <<EOF
${IDENTITY}
EOF
      local ENVIRON_PROJECT_NUMBER
      ENVIRON_PROJECT_NUMBER=$(gcloud projects describe "${ENVIRON_PROJECT_ID}" --format="value(projectNumber)")
      local PROJECT_ID="${ENVIRON_PROJECT_ID}"
      local CLUSTER_NAME="${HUB_MEMBERSHIP_ID}"
      local CLUSTER_LOCATION="us-central1-a"
      local MESH_ID="proj-${ENVIRON_PROJECT_NUMBER}"

      kpt pkg get https://github.com/GoogleCloudPlatform/anthos-service-mesh-packages.git/asm@master tmp
      kpt cfg set tmp gcloud.compute.network "network${i}"
      kpt cfg set tmp gcloud.core.project "${PROJECT_ID}"
      kpt cfg set tmp gcloud.project.environProjectNumber "${ENVIRON_PROJECT_NUMBER}"
      kpt cfg set tmp gcloud.container.cluster "${CLUSTER_NAME}"
      kpt cfg set tmp gcloud.compute.location "${CLUSTER_LOCATION}"
      kpt cfg set tmp anthos.servicemesh.rev "${ASM_REVISION_LABEL}"
      kpt cfg set tmp anthos.servicemesh.tag "${TAG}"
      kpt cfg set tmp anthos.servicemesh.hub "${HUB}"
      kpt cfg set tmp anthos.servicemesh.hubMembershipID "${HUB_MEMBERSHIP_ID}"
      kpt cfg set tmp gcloud.project.environProjectID "${ENVIRON_PROJECT_ID}"

      echo "----------Istio Operator YAML and Hub Overlay YAML----------"
      cat "tmp/istio/istio-operator.yaml"
      cat "tmp/istio/options/hub-meshca.yaml"

      echo "----------Generating expansion gw YAML----------"
      samples/multicluster/gen-eastwest-gateway.sh \
        --mesh "${MESH_ID}" \
        --cluster "${CLUSTER_NAME}" \
        --network "network${i}" > "tmp/eastwest-gateway.yaml"
      cat "tmp/eastwest-gateway.yaml"
      istioctl install -y --kubeconfig="${MC_CONFIGS[$i]}" --context=cluster -f "tmp/istio/istio-operator.yaml" -f "tmp/istio/options/hub-meshca.yaml" -f "tmp/eastwest-gateway.yaml"
    else
      cat <<EOF | istioctl install -y --kubeconfig="${MC_CONFIGS[$i]}" -f -
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: asm-multicloud
  revision: ${ASM_REVISION_LABEL}
  hub: ${HUB}
  tag: ${TAG}
  values:
    global:
      meshID: ${MESH_ID}
      multiCluster:
        clusterName: cluster${i}
      network: network${i}
EOF
      # eastwest gateway is not needed for single cluster installation
      if [[ "${CLUSTER_TOPOLOGY}" != "sc" ]]; then
        install_expansion_gw "${MESH_ID}" "cluster${i}" "network${i}" "${ASM_REVISION_LABEL}" "${HUB}" "${TAG}" "${MC_CONFIGS[$i]}"
      fi
    fi
    # set default network for the cluster, allow detecting network of non-injected pods
    kubectl --kubeconfig="${MC_CONFIGS[$i]}" label namespace istio-system topology.istio.io/network="network${i}"
    expose_services "${MC_CONFIGS[$i]}"
    configure_validating_webhook "${ASM_REVISION_LABEL}" "${MC_CONFIGS[$i]}"

    if [[ "${CLUSTER_TYPE}" == "gke-on-prem" ]]; then
      onprem::configure_external_ip "${MC_CONFIGS[$i]}"
    fi

  done

  configure_remote_secrets
}

# Outputs YAML to the given file, in the structure of []cluster.Config to inform the test framework of details about
# each cluster under test. cluster.Config is defined in pkg/test/framework/components/cluster/factory.go.
# Parameters: $1 - path to the file to append to
# Depends on env var ${KUBECONFIG} and that ASM is already installed (istio-system is present)
function multicloud::gen_topology_file() {
    local TOPO_FILE="$1"; shift
    local KUBECONFIGPATHS
    IFS=":" read -r -a KUBECONFIGPATHS <<< "${KUBECONFIG}"

    # Each kubeconfig file should have one and only one cluster context.
    for i in "${!KUBECONFIGPATHS[@]}"; do
      local CLUSTER_NAME
      CLUSTER_NAME=$(kubectl -n istio-system get pod -l app=istiod -o json --kubeconfig="${KUBECONFIGPATHS[$i]}" | \
        jq -r '.items[0].spec.containers[0].env[] | select(.name == "CLUSTER_ID") | .value')

      cat <<EOF >> "${TOPO_FILE}"
- clusterName: "${CLUSTER_NAME}"
  kind: Kubernetes
  meta:
    kubeconfig: "${KUBECONFIGPATHS[$i]}"
EOF
      echo "  network: network${i}" >> "${TOPO_FILE}"
    done
}

# Exports variable ASM_REVISION_LABEL with the value being a function of istioctl client version.
function create_asm_revision_label() {
  local ISTIO_CLIENT_TAG
  ISTIO_CLIENT_TAG=$(istioctl version --remote=false -o json | jq -r '.clientVersion.tag')
  IFS='-'; read -r -a tag_tokens <<< "${ISTIO_CLIENT_TAG}"
  unset IFS
  ASM_REVISION_LABEL="asm-${tag_tokens[0]}-${tag_tokens[1]}"
  ASM_REVISION_LABEL=${ASM_REVISION_LABEL//.}
  export ASM_REVISION_LABEL
}

# Creates ca certs on the cluster
# $1    kubeconfig
function install_certs() {
  kubectl create secret generic cacerts -n istio-system \
    --kubeconfig="$1" \
    --from-file=samples/certs/ca-cert.pem \
    --from-file=samples/certs/ca-key.pem \
    --from-file=samples/certs/root-cert.pem \
    --from-file=samples/certs/cert-chain.pem
}

# Creates remote secrets for each cluster pair for all the clusters under test
function configure_remote_secrets() {
  for i in "${!MC_CONFIGS[@]}"; do
    for j in "${!MC_CONFIGS[@]}"; do
      if [[ "$i" != "$j" ]]; then
        istioctl x create-remote-secret \
          --kubeconfig="${MC_CONFIGS[$j]}" \
          --name="secret-${j}" \
        | kubectl apply --kubeconfig="${MC_CONFIGS[$i]}" -f -
      fi
    done
  done
}

# on-prem specific fucntion to configure external ips for the gateways
# Parameters:
# $1    kubeconfig
function onprem::configure_external_ip() {
  local HERC_ENV_ID
  HERC_ENV_ID=$(echo "$1" | rev | cut -d '/' -f 2 | rev)
  local INGRESS_ID=\"lb-test-ip\"
  local EXPANSION_ID=\"expansion-ip\"
  local INGRESS_IP

  echo "Installing herc CLI..."
  gsutil cp "gs://anthos-hercules-public-artifacts/herc/latest/herc" "/usr/local/bin/" && chmod 755 "/usr/local/bin/herc"

  INGRESS_IP=$(herc getEnvironment "${HERC_ENV_ID}" -o json | \
    jq -r ".environment.resources.vcenter_server.datacenter.networks.fe.ip_addresses.${INGRESS_ID}.ip_address")

  # Request additional external IP for expansion gw
  local HERC_PARENT
  HERC_PARENT=$(herc getEnvironment "${HERC_ENV_ID}" | \
    grep "name: environments.*lb-test-ip$" | awk -F' ' '{print $2}' | sed 's/\/ips\/lb-test-ip//')
  local EXPANSION_IP
  EXPANSION_IP=$(herc getEnvironment "${HERC_ENV_ID}" -o json | \
    jq -r ".environment.resources.vcenter_server.datacenter.networks.fe.ip_addresses.${EXPANSION_ID}.ip_address")
  if [[ -z "${EXPANSION_IP}" || "${EXPANSION_IP}" == "null" ]]; then
    echo "Requesting herc for expansion IP"
    herc allocateIPs --parent "${HERC_PARENT}" -f "${CONFIG_DIR}/herc/expansion-ip.yaml"
    EXPANSION_IP=$(herc getEnvironment "${HERC_ENV_ID}" -o json | \
      jq -r ".environment.resources.vcenter_server.datacenter.networks.fe.ip_addresses.${EXPANSION_ID}.ip_address")
  else
    echo "Using ${EXPANSION_IP} as the expansion IP"
  fi

  # Inject the external IPs for GWs
  echo "----------Configuring external IP for ingress gw----------"
  kubectl patch svc istio-ingressgateway -n istio-system \
    --type='json' -p '[{"op": "add", "path": "/spec/loadBalancerIP", "value": "'"${INGRESS_IP}"'"}]' \
    --kubeconfig="$1"
  echo "----------Configuring external IP for expansion gw----------"
  kubectl patch svc istio-eastwestgateway -n istio-system \
    --type='json' -p '[{"op": "add", "path": "/spec/loadBalancerIP", "value": "'"${EXPANSION_IP}"'"}]' \
    --kubeconfig="$1"
}

# Installs expansion gw
# Parameters:
# $1    mesh id
# $2    cluster
# $3    network
# $4    revision
# $5    hub
# $6    tag
# $7    kubeconfig
function install_expansion_gw() {
  echo "----------Installing expansion gw----------"
  samples/multicluster/gen-eastwest-gateway.sh \
    --mesh "$1" \
    --cluster "$2" \
    --network "$3" \
    --revision "$4" \
  | istioctl install -y -f - \
    --set hub="$5" \
    --set tag="$6" \
    --kubeconfig="$7"
}

# Exposes service in istio-system ns
# Parameters:
# $1    kubeconfig
function expose_services() {
  echo "----------Exposing Services----------"
  kubectl apply -n istio-system -f samples/multicluster/expose-services.yaml --kubeconfig="$1"
}

# Configures validating webhook for istiod
# Parameters:
# $1    revision
# $2    kubeconfig
function configure_validating_webhook() {
  echo "----------Configuring validating webhook----------"
  cat <<EOF | kubectl apply --kubeconfig="$2" -f -
apiVersion: v1
kind: Service
metadata:
 name: istiod
 namespace: istio-system
 labels:
   istio.io/rev: ${1}
   app: istiod
   istio: pilot
   release: istio
spec:
 ports:
   - port: 15010
     name: grpc-xds # plaintext
     protocol: TCP
   - port: 15012
     name: https-dns # mTLS with k8s-signed cert
     protocol: TCP
   - port: 443
     name: https-webhook # validation and injection
     targetPort: 15017
     protocol: TCP
   - port: 15014
     name: http-monitoring # prometheus stats
     protocol: TCP
 selector:
   app: istiod
   istio.io/rev: ${1}
EOF
}

# Construct correct HTTP_PROXY env value used by baremetal according to the tunnel
function init_baremetal_http_proxy() {
  for CONFIG in "${MC_CONFIGS[@]}"; do
    CLUSTER_ARTIFACTS_PATH=${CONFIG%/*}
    local PORT_NUMBER
    read -r PORT_NUMBER BOOTSTRAP_HOST_SSH_USER <<<"$(grep "localhost" "${CLUSTER_ARTIFACTS_PATH}/tunnel.sh" | sed 's/.*\-L\([0-9]*\):localhost.* \(root@[0-9]*\.[0-9]*\.[0-9]*\.[0-9]*\) -N/\1 \2/')"
    HTTP_PROXY="localhost:${PORT_NUMBER}"
    HTTPS_PROXY="${HTTP_PROXY}"
    echo "----------PROXY env----------"
    echo "HTTP_PROXY: ${HTTP_PROXY}, HTTPS_PROXY: ${HTTPS_PROXY}"
  done
  BOOTSTRAP_HOST_SSH_KEY="${CLUSTER_ARTIFACTS_PATH}/id_rsa"
  echo "----------BM Cluster env----------"
  echo "CLUSTER_ARTIFACTS_PATH: ${CLUSTER_ARTIFACTS_PATH}, BOOTSTRAP_HOST_SSH_USER: ${BOOTSTRAP_HOST_SSH_USER}, BOOTSTRAP_HOST_SSH_KEY: ${BOOTSTRAP_HOST_SSH_KEY}"
  export CLUSTER_ARTIFACTS_PATH
  # Used by ingress related tests
  export BOOTSTRAP_HOST_SSH_USER
  export BOOTSTRAP_HOST_SSH_KEY
}

# Sources the environment variables created by test infra needed to connect
# to the clusters under test.
# KUBECONFIG value will be preserved.
function aws::init() {
  local OLD_KC="${KUBECONFIG}"
  for CONFIG in "${MC_CONFIGS[@]}"; do
    CLUSTER_TB_ID=${CONFIG//*kubetest2-tailorbird\//}
    CLUSTER_TB_ID=${CLUSTER_TB_ID//\/.kube*/}

    RESOURCE_DIR="${ARTIFACTS}/.kubetest2-tailorbird/${CLUSTER_TB_ID}"
    # shellcheck disable=SC1090
    source "${RESOURCE_DIR}/resource_vars"

    HTTPS_PROXY="${HTTP_PROXY}"
    export HTTPS_PROXY

    CLUSTER_ARTIFACTS_PATH="${RESOURCE_DIR}"
    read -r BOOTSTRAP_HOST_SSH_USER <<<"$(grep "localhost" "${CLUSTER_ARTIFACTS_PATH}/tunnel-script.sh" | sed 's/.* \(ubuntu@.*compute.amazonaws.com\) .*/\1/')"
  done
  BOOTSTRAP_HOST_SSH_KEY="${CLUSTER_ARTIFACTS_PATH}/.ssh/anthos-gke"
  echo "----------AWS Cluster env----------"
  echo "CLUSTER_ARTIFACTS_PATH: ${CLUSTER_ARTIFACTS_PATH}, BOOTSTRAP_HOST_SSH_USER: ${BOOTSTRAP_HOST_SSH_USER}, BOOTSTRAP_HOST_SSH_KEY; ${BOOTSTRAP_HOST_SSH_KEY}"
  export CLUSTER_ARTIFACTS_PATH
  # Used by ingress related tests
  export BOOTSTRAP_HOST_SSH_USER
  export BOOTSTRAP_HOST_SSH_KEY
  # Increase proxy's max connection setup to avoid too many connections error
  ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i "${BOOTSTRAP_HOST_SSH_KEY}" "${BOOTSTRAP_HOST_SSH_USER}" "sudo sed -i 's/#max-client-connections.*/max-client-connections 512/' '/etc/privoxy/config'"
  ssh -o UserKnownHostsFile=/dev/null -o StrictHostKeyChecking=no -i "${BOOTSTRAP_HOST_SSH_KEY}" "${BOOTSTRAP_HOST_SSH_USER}" "sudo systemctl restart privoxy.service"

  # reset to the existing value because resource_vars for each
  # cluster can override the normalized KUBECONFIGs, including
  # adding back the management cluster config.
  KUBECONFIG="${OLD_KC}"
}

# Creates virtual machines, registers them with the cluster and install the test echo app.
# Parameters: $1 - the name of a directory in echo-vm-provisioner describing how to setup the VMs.
#             $2 - the context of the cluster VMs will connect to
function setup_asm_vms() {
  local VM_DIR="${CONFIG_DIR}/echo-vm-provisioner/$1"
  local CONTEXT=$2
  local VM_DISTRO="${3:-debian-10}"
  local IMAGE_PROJECT="${4:-debian-cloud}"

  IFS="_" read -r -a VALS <<< "${CONTEXT}"
  local PROJECT_ID=${VALS[1]}
  local CLUSTER_LOCATION=${VALS[2]}
  local CLUSTER_NAME=${VALS[3]}

  local PROJECT_NUMBER
  PROJECT_NUMBER=$(gcloud projects describe "${PROJECT_ID}" --format="value(projectNumber)")
  local REVISION
  REVISION="$(kubectl --context="${CONTEXT}" -n istio-system get service istio-eastwestgateway -ojsonpath='{.metadata.labels.istio\.io/rev}')"

  export ISTIO_OUT="$PWD/out"
  setup_static_vms "${CONTEXT}" "${CLUSTER_NAME}" "${CLUSTER_LOCATION}" "${PROJECT_NUMBER}" "${REVISION}" "${VM_DIR}" "${VM_DISTRO}" "${IMAGE_PROJECT}"
}

# Add function call to trap
# Parameters: $1 - Function to call
#             $2...$n - Signals for trap
function add_trap {
  local cmd=$1
  shift
  for trap_signal in "$@"; do
    local current_trap
    current_trap="$(trap -p "$trap_signal" | cut -d\' -f2)"
    local new_cmd="($cmd)"
    [[ -n "${current_trap}" ]] && new_cmd="${current_trap};${new_cmd}"
    trap -- "${new_cmd}" "$trap_signal"
  done
}

# Split the disabled tests around pipe delim and individually sets the
# --istio.test.skip flag for each. This is needed to enable regex and
# fine grained test disabling.
# Parameters:
# $1    Disabled tests pipe-separated string
function apply_skip_disabled_tests() {
  if [[ -n "${1}" ]]; then
    IFS='|' read -r -a TESTS_TO_SKIP <<< "${1}"
    for test in "${TESTS_TO_SKIP[@]}"; do
      INTEGRATION_TEST_FLAGS+=" --istio.test.skip=\"${test}\""
    done
  fi
}

function install_asm_on_baremetal() {
  local MESH_ID="test-mesh"
  for i in "${!MC_CONFIGS[@]}"; do
    install_certs "${MC_CONFIGS[$i]}"
    echo "----------Installing ASM----------"
    cat <<EOF | istioctl install -y --kubeconfig="${MC_CONFIGS[$i]}" -f -
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: asm-multicloud
  revision: ${ASM_REVISION_LABEL}
  hub: ${HUB}
  tag: ${TAG}
  values:
    global:
      meshID: ${MESH_ID}
      multiCluster:
        clusterName: cluster${i}
      network: network${i}
EOF
    configure_validating_webhook "${ASM_REVISION_LABEL}" "${MC_CONFIGS[$i]}"
  done
}

# Install ASM User Auth on the cluster.
function install_asm_user_auth() {
  kubectl create namespace asm-user-auth
  kubectl label namespace asm-user-auth istio-injection=enabled --overwrite

  kubectl create namespace userauth-test
  kubectl label namespace userauth-test istio-injection=enabled --overwrite
  # TODO(b/182914654): deploy app in go code
  kubectl -n userauth-test apply -f https://raw.githubusercontent.com/istio/istio/master/samples/httpbin/httpbin.yaml

  kubectl wait --for=condition=Ready --timeout=2m --namespace=userauth-test --all pod

  # Create the kubernetes secret for the encryption and signing key.
  # shellcheck disable=SC2140
  kubectl create secret generic secret-key  \
       --from-file="session_cookie.key"="${CONFIG_DIR}/user-auth/aes_symmetric_key.json"  \
       --from-file="rctoken.key"="${CONFIG_DIR}/user-auth/rsa_signing_key.json"  \
       --namespace=asm-user-auth

  kpt pkg get https://github.com/GoogleCloudPlatform/asm-user-auth.git/pkg@main "${CONFIG_DIR}/user-auth/pkg"

  kubectl apply -f "${CONFIG_DIR}/user-auth/pkg/asm_user_auth_config_v1alpha1.yaml"

  # The following account is from IAP team
  # TODO(b/182940034): use ASM owned account once created
  OIDC_CLIENT_ID="$(jq -r ".client_id" "${CONFIG_DIR}/user-auth/userauth_oidc.json")"
  OIDC_CLIENT_SECRET="$(jq -r ".client_secret" "${CONFIG_DIR}/user-auth/userauth_oidc.json")"
  OIDC_ISSUER_URI="$(jq -r ".issuer" "${CONFIG_DIR}/user-auth/userauth_oidc.json")"

  # TODO(b/182918059): Fetch image from GCR release repo and GitHub packet.
  USER_AUTH_IMAGE="gcr.io/gke-release-staging/asm/asm_user_auth:staging"
  kpt cfg set "${CONFIG_DIR}/user-auth/pkg" anthos.servicemesh.user-auth.image "${USER_AUTH_IMAGE}"
  kpt cfg set "${CONFIG_DIR}/user-auth/pkg" anthos.servicemesh.user-auth.oidc.clientID "${OIDC_CLIENT_ID}"
  kpt cfg set "${CONFIG_DIR}/user-auth/pkg" anthos.servicemesh.user-auth.oidc.clientSecret "${OIDC_CLIENT_SECRET}"
  kpt cfg set "${CONFIG_DIR}/user-auth/pkg" anthos.servicemesh.user-auth.oidc.issuerURI "${OIDC_ISSUER_URI}"
  kubectl apply -R -f "${CONFIG_DIR}/user-auth/pkg/"

  kubectl wait --for=condition=Ready --timeout=2m --namespace=asm-user-auth --all pod
  kubectl apply -f "${CONFIG_DIR}/user-auth/httpbin-route.yaml"
}

# Cleanup ASM User Auth manifest.
function cleanup_asm_user_auth() {
  kubectl delete -f "${CONFIG_DIR}/user-auth/pkg/asm_user_auth_config_v1alpha1.yaml"
  kubectl delete -f "${CONFIG_DIR}/user-auth/pkg/cluster_role_binding.yaml"
  kubectl delete -f "${CONFIG_DIR}/user-auth/pkg/ext_authz.yaml"
  kubectl -n userauth-test delete -f https://raw.githubusercontent.com/istio/istio/master/samples/httpbin/httpbin.yaml
  rm -rf "${CONFIG_DIR}/user-auth/pkg"
  kubectl delete ns asm-user-auth userauth-test
}

# Download dependencies: Chrome, Selenium
function download_user_auth_dependencies() {
  # need this mkdir for installing jre: https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=863199
  mkdir -p /usr/share/man/man1
  # TODO(b/182939536): add apt-get to https://github.com/istio/tools/blob/master/docker/build-tools/Dockerfile
  apt-get update && apt-get install -y --no-install-recommends unzip openjdk-11-jre xvfb chromium-browser

  mkdir "${CONFIG_DIR}/user-auth/dependencies"

  LASTCHANGE_URL="https://www.googleapis.com/download/storage/v1/b/chromium-browser-snapshots/o/Linux_x64%2FLAST_CHANGE?alt=media"
  REVISION=$(curl -s -S "${LASTCHANGE_URL}")
  CHROMIUM_URL="https://www.googleapis.com/download/storage/v1/b/chromium-browser-snapshots/o/Linux_x64%2F$REVISION%2Fchrome-linux.zip?alt=media"
  DRIVER_URL="https://www.googleapis.com/download/storage/v1/b/chromium-browser-snapshots/o/Linux_x64%2F$REVISION%2Fchromedriver_linux64.zip?alt=media"
  SELENIUM_URL="https://selenium-release.storage.googleapis.com/3.141/selenium-server-standalone-3.141.59.jar"

  curl -# "${CHROMIUM_URL}" > "${CONFIG_DIR}/user-auth/dependencies/chrome-linux.zip"
  curl -# "${DRIVER_URL}" > "${CONFIG_DIR}/user-auth/dependencies/chromedriver-linux.zip"

  unzip "${CONFIG_DIR}/user-auth/dependencies/chrome-linux.zip" -d "${CONFIG_DIR}/user-auth/dependencies"
  unzip "${CONFIG_DIR}/user-auth/dependencies/chromedriver-linux.zip" -d "${CONFIG_DIR}/user-auth/dependencies"
  rm -rf "${CONFIG_DIR}/user-auth/dependencies/chrome-linux.zip" "${CONFIG_DIR}/user-auth/dependencies/chromedriver-linux.zip"

  curl -# "${SELENIUM_URL}" > "${CONFIG_DIR}/user-auth/dependencies/selenium-server.jar"

  # need below for DevToolsActivePorts error, https://yaqs.corp.google.com/eng/q/5322136407900160
  echo "export DISPLAY=:20" >> ~/.bashrc
}

# Cleanup dependencies
function cleanup_user_auth_dependencies() {
  rm -rf "${CONFIG_DIR}/user-auth/dependencies"
}
