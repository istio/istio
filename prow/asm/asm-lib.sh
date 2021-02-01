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

# The GCP project we use when testing with multicloud clusters, or when we need to
# hold some GCP resources that are centrally managed.
export CENTRAL_GCP_PROJECT="istio-prow-build"

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

# Prepare images (istiod, proxyv2) required for the managed control plane e2e test.
# Depends on env var ${HUB} and ${TAG}
function prepare_images_for_managed_control_plane() {
  # Build images from the current branch and push the images to gcr.
  HUB="${HUB}" TAG="${TAG}" make push.docker.cloudrun push.docker.proxyv2
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
  gcloud beta container images delete "${HUB}/app:${TAG}" --force-delete-tags --quiet
  gcloud beta container images delete "${HUB}/pilot:${TAG}" --force-delete-tags --quiet
  gcloud beta container images delete "${HUB}/proxyv2:${TAG}" --force-delete-tags --quiet
  gcloud beta container images delete "${HUB}/stackdriver-prometheus-sidecar:${TAG}" --force-delete-tags --quiet
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
#             $2 - a string of k8s contexts
function set_multicloud_permissions() {
  local GCR_PROJECT_ID="$1"
  IFS="," read -r -a CONTEXTS <<< "$2"

  local SECRETNAME="test-gcr-secret"
  for i in "${!CONTEXTS[@]}"; do
    kubectl --context="${CONTEXTS[$i]}" create ns istio-system
    kubectl create secret -n istio-system docker-registry "${SECRETNAME}" \
      --docker-server=https://gcr.io \
      --docker-username=_json_key \
      --docker-email="$(gcloud config get-value account)" \
      --docker-password="$(cat "${GOOGLE_APPLICATION_CREDENTIALS}")" \
      --context="${CONTEXTS[$i]}"

    while read -r SA; do
      add_image_pull_secret_to_sa "${SA}" "${SECRETNAME}" "${CONTEXTS[$i]}"
    done <<EOF
default
istio-ingressgateway-service-account
istio-reader-service-account
istiod-service-account
EOF

  done
}

# Add the imagePullSecret to the service account
# Parameters: $1 - service account
#             $2 - secret name
#             $3 - cluster context
function add_image_pull_secret_to_sa() {
  cat <<EOF | kubectl --context="${3}" apply -f -
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
  if ! gcloud beta privateca roots list --project "${CENTRAL_GCP_PROJECT}" --location "${ROOT_CA_LOC}" | grep -q "${ROOT_CA_ID}"; then
    echo "Creating root CA ${ROOT_CA_ID}..."
    gcloud beta privateca roots create "${ROOT_CA_ID}" \
      --location "${ROOT_CA_LOC}" \
      --project "${CENTRAL_GCP_PROJECT}" \
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
        --project "${CENTRAL_GCP_PROJECT}" \
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
  if gcloud beta privateca roots list --project "${CENTRAL_GCP_PROJECT}" --location "${ROOT_CA_LOC}" | grep -q "${ROOT_CA_ID}"; then
    echo "Purging root CA $ROOT_CA_ID.."
    purge-ca "roots" "${ROOT_CA_ID}" "${ROOT_CA_LOC}" "${CENTRAL_GCP_PROJECT}"
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
    gcloud beta privateca "$1" delete "$2" --location="$3" --project="$4" --quiet
  fi
}

# Install ASM on the clusters.
# Parameters: $1 - PKG: Path of Kpt package
#             $2 - CA: CITADEL, MESHCA or PRIVATECA
#             $3 - WIP: GKE or HUB
#             $4 - array of k8s contexts
# Depends on env var ${HUB} and ${TAG}
function install_asm() {
  local PKG="$1"; shift
  local CA="$1"; shift
  local WIP="$1"; shift
  local CONTEXTS=("${@}")
  local CUSTOM_OVERLAY="${PKG}/overlay/default.yaml"

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
      export MESH_ID="proj-${ENVIRON_PROJECT_NUMBER}"
      echo "Environ project ID: ${PROJECT_ID}, project number: ${ENVIRON_PROJECT_NUMBER}"
    fi

    if [[ "${CA}" == "MESHCA" || "${CA}" == "PRIVATECA" ]]; then
      INSTALL_ASM_CA="mesh_ca"
      if [[ "${CLUSTER_TOPOLOGY}" == "MULTIPROJECT_MULTICLUSTER" ]]; then
        TRUSTED_GCP_PROJECTS=""
        for j in "${!CONTEXTS[@]}"; do
          if [[ "$i" != "$j" ]]; then
            IFS="_" read -r -a TMP <<< "${CONTEXTS[$j]}"
            TRUSTED_GCP_PROJECTS="$TRUSTED_GCP_PROJECTS --trusted_gcp_project ${TMP[1]}"
          fi
        done
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
      kubectl create namespace istio-system --context="${CONTEXTS[$i]}"
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
      # Setup NAT to grant private nodes outbound internet access
      gcloud compute routers create test-router \
        --network default \
        --region us-central1 || echo "test-router already exists"
      gcloud compute routers nats create test-nat \
        --router=test-router \
        --auto-allocate-nat-external-ips \
        --nat-all-subnet-ip-ranges \
        --router-region=us-central1 \
        --enable-logging || echo "test-nat already exists"
    fi

    # INSTALL_ASM_COMMIT is from a branch master-prow with more CI options on install_asm (not yet exposed to end users)
    # TODO(taohe@): if install_asm is updated for prow tests, update the commit ID accordingly.
    INSTALL_ASM_COMMIT="dbf24423b9fb44068f792669f36abbd97d606a48"
    curl -O https://raw.githubusercontent.com/GoogleCloudPlatform/anthos-service-mesh-packages/"${INSTALL_ASM_COMMIT}"/scripts/asm-installer/install_asm
    TRUSTED_GCP_PROJECTS="${TRUSTED_GCP_PROJECTS:=}"
    CUSTOM_CA_FLAGS="${CUSTOM_CA_FLAGS:=}"
    chmod +x install_asm

    # Env variable for CI options on install_asm
    export _CI_ISTIOCTL_REL_PATH="$PWD/out/linux_amd64/istioctl"
    export _CI_ASM_IMAGE_LOCATION="${HUB}"
    export _CI_ASM_IMAGE_TAG="${TAG}"
    export _CI_ASM_KPT_BRANCH=master-prow
    export _CI_NO_VALIDATE=1
    export _CI_NO_REVISION=1

    local SERVICE_ACCOUNT
    SERVICE_ACCOUNT=$(gcloud config list --format "value(core.account)")
    if [[ "${WIP}" != "HUB" ]]; then
      if [[ "${CLUSTER_TOPOLOGY}" == "MULTIPROJECT_MULTICLUSTER" ]]; then
        eval ./install_asm \
          --project_id "${PROJECT_ID}" \
          --cluster_name "${CLUSTER}" \
          --cluster_location "${LOCATION}" \
          --ca ${INSTALL_ASM_CA} \
          "${CUSTOM_CA_FLAGS}" \
          --mode install \
          --enable_gcp_apis \
          --option multiproject \
          --option audit-authorizationpolicy \
          --environ_project_number "${ENVIRON_PROJECT_NUMBER}" \
          "${TRUSTED_GCP_PROJECTS}" \
          --custom_overlay "${CUSTOM_OVERLAY}" \
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
          --service_account "${SERVICE_ACCOUNT}" \
          --key-file "${GOOGLE_APPLICATION_CREDENTIALS}" \
          --verbose
      fi
    else
      if [[ "${CLUSTER_TOPOLOGY}" == "MULTIPROJECT_MULTICLUSTER" ]]; then
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
          --environ_project_number "${ENVIRON_PROJECT_NUMBER}" \
          "${TRUSTED_GCP_PROJECTS}" \
          --custom_overlay "${CUSTOM_OVERLAY}" \
          --option audit-authorizationpolicy \
          --service_account "${SERVICE_ACCOUNT}" \
          --key-file "${GOOGLE_APPLICATION_CREDENTIALS}" \
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
          --custom_overlay "${CUSTOM_OVERLAY}" \
          --option audit-authorizationpolicy \
          --service_account "${SERVICE_ACCOUNT}" \
          --key-file "${GOOGLE_APPLICATION_CREDENTIALS}" \
          --verbose
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
      fi
    done
  done
}

# Install ASM managed control plane.
# Parameters: $1 - array of k8s contexts
# Depends on env var ${HUB} and ${TAG}
function install_asm_managed_control_plane() {
  local CONTEXTS=("${@}")
  IFS="_" read -r -a VALS <<< "${CONTEXTS[0]}"
  local PROJECT_ID="${VALS[1]}"
  local CLUSTER_LOCATION="${VALS[2]}"
  local CLUSTER_NAME="${VALS[3]}"
  # Obtain 1.8 Scriptaro for ASM MCP instead of the latest from github.
  # We'll manually bump up the version here upon new ASM release.
  curl https://storage.googleapis.com/csm-artifacts/asm/install_asm_1.8 > install_asm
  sed -i 's/meshconfig\.googleapis\.com/staging-meshconfig.sandbox.googleapis.com/g' install_asm
  chmod +x install_asm

  # TODO(ruigu): A hacky walkaround for b/178629331.
  # Scriptaro will fail during Gateway installation. We'll ignore the error.
  # After that, we manually modify the env in the Gateway deployment.
  sed -i 's/retry 5 run/retry 1 run/g' install_asm

  _CI_CLOUDRUN_IMAGE_HUB="${HUB}/cloudrun" _CI_CLOUDRUN_IMAGE_TAG="${TAG}" "./install_asm" \
    --mode install \
    --project_id "${PROJECT_ID}" \
    --cluster_location "${CLUSTER_LOCATION}" \
    --cluster_name "${CLUSTER_NAME}" \
    --managed \
    --service_account "prow-gob-storage@istio-prow-build.iam.gserviceaccount.com" \
    --key_file "/etc/service-account/service-account.json" \
    --enable_cluster_labels \
    --verbose || true

  # TODO(ruigu): Remove the following hack when Scriptaro is ready.
  # Manually set ISTIO_META_CLOUDRUN_ADDR env to the cloudrun address.
  # http://doc/163UaaQcYzCSHSewolZOq7X1DYFRfblY0CZkIBQczZO4#heading=h.xh7sh1ybhy43
  # shellcheck disable=SC2155
  local CLOUDRUN_ADDR=$(kubectl get mutatingwebhookconfigurations istiod-asm-managed -ojson | jq ".webhooks[0].clientConfig.url" | cut -d'/' -f3)
  kubectl set env deployment/istio-ingressgateway ISTIO_META_CLOUDRUN_ADDR="${CLOUDRUN_ADDR}" -n istio-system
}

# Install ASM on the clusters.
# Parameters: $1 - PKG: Path of Kpt package
#             $2 - CA: CITADEL, MESHCA or PRIVATECA
#             $3 - WIP: GKE or HUB
#             $4 - array of k8s contexts
# Depends on env var ${HUB} and ${TAG}
# TODO remove this function once b/176177944 is fixed
function install_asm_on_multicloud() {
  exit 0
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
