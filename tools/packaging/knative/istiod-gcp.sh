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

set -ex # Print out all commands, exit on failed commands

if [[ -z ${IN_CLUSTER} ]] ; then
  if [[ -n ${PROJECT} ]]; then
    # Retry for 3~4 minutes to wait for the IAM permission to get fully
    # populated (See b/169186377, b/186644991).
    START=$(date +%s)
    while true; do
      CODE="$(curl --write-out '%{http_code}' --silent --show-error --output /tmp/curlout "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token" -H "Metadata-Flavor: Google")"
      if [[ "${CODE}" == "200" ]] ; then
        # Token request was successful, exit the loop.
        break
      fi
      echo "Response code: ${CODE}"
      echo "Response body:"
      cat /tmp/curlout || echo "<no body>"
      if (( $(date +%s) - START > 3 * 60 + 30 )); then
        echo 'failed to request the token'
        exit 1
      fi
      sleep 5
    done

    # We must use cluster labels unless explicitly set.
    # TODO: Investigate if there is a need to retry the gcloud command below
    # to mitigate flakiness.
    if [[ -z "${GKEHUB}" ]]; then
      GKEHUB="$(gcloud container clusters describe "${CLUSTER}" --zone "${ZONE}" --project "${PROJECT}" --format="value(resourceLabels[hub])" --billing-project "${PROJECT}")"
    fi

    # Retry for 5 seconds in case there's flakiness in the gcloud command,
    # though we expect the command to succeed on the first try. We have
    # seen random errors like "ERROR: gcloud crashed (MetadataServerException):".
    RET=1
    START=$(date +%s)
    while true; do
      if [[ "${GKEHUB}" == "1" ]]; then
        RET="$(gcloud beta container hub memberships get-credentials "${CLUSTER}" --project "${PROJECT}" --billing-project "${PROJECT}" || echo 1)"
      else
        RET="$(gcloud container clusters get-credentials "${CLUSTER}" --zone "${ZONE}" --project "${PROJECT}" --billing-project "${PROJECT}" || echo 1)"
      fi
      if [[ "${RET}" -eq "0" ]] ; then
        # get-credentials command was successful, exit the loop.
        break
      fi
      if (( $(date +%s) - START > 5 )); then
        echo 'gcloud container clusters get-credentials failed'
        exit 1
      fi
      sleep 5
    done

    # TODO: check secret manager for a .kubeconfig - use it for non-GKE projects AND ingress secrets
    # AND citadel root CA
  fi
fi

# In-cluster ASM options, set by install_asm or feature controller or user.
# TODO: istiod should also watch the configmap, react dynamically where possible (or exit)
ASM_OPTS="$(kubectl -n istio-system get --ignore-not-found cm asm-options -o jsonpath='{.data.ASM_OPTS}' || echo "")"

# Disable webhook config patching - manual configs used, proper DNS certs means no cert patching needed.
# If running in KNative without a DNS cert - we may need it back, but we should require DNS certs.
# Even if user doesn't have a DNS name - they can still use an self-signed root and add it to system trust,
# to simplify
export VALIDATION_WEBHOOK_CONFIG_NAME=
export INJECTION_WEBHOOK_CONFIG_NAME=

# No mTLS for control plane
export USE_TOKEN_FOR_CSR=true
export USE_TOKEN_FOR_XDS=true

# Disable the DNS-over-TLS server - no ports
export DNS_ADDR=

export REVISION="${REV:-asm-managed}"

# TODO: should be auto-set now, verify safe to remove
export GKE_CLUSTER_URL=https://container.googleapis.com/v1/projects/${PROJECT}/locations/${ZONE}/clusters/${CLUSTER}

export CLUSTER_ID=cn-${PROJECT}-${ZONE}-${CLUSTER}

# GCP_METADATA is used by gcp_metadata package and provides metadata for telemetry.
export GCP_METADATA="${PROJECT}|${PROJECT_NUMBER}|${CLUSTER}|${ZONE}"

# Check for required arguments
: "${K_REVISION:?K_REVISION not set or is empty}"
# Revision is equivalent with a deployment - unfortunately we can't get instance id.
POD_NAME="${K_REVISION}-$(date +%N)"
export POD_NAME

# Merge a shared asm config map, if user provides it
export SHARED_MESH_CONFIG=asm

# The auth provider for XDS (e.g., gcp). The default is empty.
export XDS_AUTH_PROVIDER="${XDS_AUTH_PROVIDER:-}"

# The JWT rule for istiod JWT authenticator. The default is as expected for MCP
MCP_ENV=${MCP_ENV:-prod}
JWT_DEFAULT='{"audiences":["'${MCP_ENV}:${PROJECT}'.svc.id.goog"],"issuer":"cloud-services-platform-thetis@system.gserviceaccount.com","jwks_uri":"https://www.googleapis.com/service_accounts/v1/metadata/jwk/cloud-services-platform-thetis@system.gserviceaccount.com"}'
export JWT_RULE="${JWT_RULE:-${JWT_DEFAULT}}"
CA=${CA:-1}
ASM=${ASM:-1}

export XDS_AUTH_PLAINTEXT=true
export XDS_TOKEN_TYPE=${XDS_TOKEN_TYPE:-Bearer}

# XDS_ADDR is the address (without the scheme, but including an explicit port)
# used for discovery. It's either the address of the Cloud Run service directly
# or the address of the MeshConfig proxy. If it's not set, then it's constructed
# using ISTIOD_DOMAIN, which is just the suffix of the url. ISTIOD_DOMAIN is
# deprecated and will eventually be removed.
if [[ -z "${XDS_ADDR}" ]]; then
  if [[ -z "${ISTIOD_DOMAIN}" ]]; then
    echo 'error: XDS_ADDR or ISTIOD_DOMAIN must be set.'
    exit 1
  fi

  # ISTIOD_DOMAIN is set, but XDS_ADDR is not set. Construct XDS_ADDR using
  # ISTIOD_DOMAIN and the service name, which will work in most circumstances
  # (e.g. when the service name is short enough to not get truncated).
  export XDS_ADDR="${K_SERVICE}${ISTIOD_DOMAIN}:443"
fi

# CLOUDRUN_ADDR is the address (without the scheme, but including an explicit
# port) of this Cloud Run service.
if [[ -z "${CLOUDRUN_ADDR}" ]]; then
  echo 'error: CLOUDRUN_ADDR must be set.'
  exit 1
fi

function useMeshCA() {
  echo "Using Mesh CA"
  export CA_ADDR=meshca.googleapis.com:443
  export TRUST_DOMAIN=${PROJECT}.svc.id.goog
  export AUDIENCE=${TRUST_DOMAIN}
  # Disable istiod's ca server because it's unused when we're using MeshCA, and
  # to avoid a race condition where multiple istiods race to create CA secrets
  # at the same time and can potentially fail.
  export ENABLE_CA_SERVER=0
}

function useIstiodCA() {
    echo "Istiod CA secret exists, using Istiod CA"
    # If not set - the template default is a made-up istiod address instead of discovery.
    # TODO: fix template
    # TODO: if we fetch MeshConfig from cluster - leave trust domain untouched.
    export CA_ADDR=${CLOUDRUN_ADDR}
    export TRUST_DOMAIN=cluster.local
    export AUDIENCE=${PROJECT}.svc.id.goog
}

if [[ "${ASM_OPTS}" == *"CA=check"* ]]; then
  if kubectl get secret -n istio-system istio-ca-secret ; then
    useIstiodCA
  else
    useMeshCA
  fi
else
  useMeshCA
fi

# Old install_asm - we expect install_asm to create istio_system and CRDs
RET="$(kubectl get ns istio-system || echo 1)"
# shellcheck disable=SC2181
if [[ "$RET" == "1" ]]; then
  if [[ "${GKEHUB}" == "1" ]]; then
    echo "Istio-system not initialized, using HUB require istio-system"
    exit 1
  fi
  echo "Initializing istio-system and CRDs, fresh cluster, old install_asm"
  kubectl create ns istio-system
  kubectl apply -f /var/lib/istio/config/gen-istio-cluster.yaml \
    --record=false --overwrite=false --force-conflicts=true --server-side
fi


# The provisioning or user needs to set it explicitly on the cluster 'cni' label, to avoid depending
# on auto-detection. Since the feature is not yet certified as stable, opt in via auto-detection or "true"
# The value of CNI is used by envsubst in values.yaml, which is used for injection, must be 'true' or 'false'.
if [[ "${ASM_OPTS}" == *"CNI=check"* ]]; then
  # shellcheck disable=SC2181
  if (kubectl -n kube-system get po -l k8s-app=istio-cni-node | grep istio-cni-node) ; then
     CNI=true
  else
     CNI=false
  fi
  export CNI
elif [[ "${ASM_OPTS}" == *"CNI=on"* ]]; then
  CNI=true
else
  CNI=false
fi
export CNI

envsubst < /etc/istio/config/mesh_template.yaml > /etc/istio/config/mesh.yaml
cat /etc/istio/config/mesh.yaml

# Istio looks for /var/lib/istio/inject/{config|values}.
# Due to the high risk of breakages if the in-cluster config map is modified we use the tested version.
# Istio doesn't officially support users editing the injection config map.
envsubst < /var/lib/istio/inject/values_template.yaml > /var/lib/istio/inject/values
cat  /var/lib/istio/inject/values

# Create file config sources for telemetry
mkdir -p /var/lib/istio/config/data
if [[ "${ASM}" == "1" ]]; then
  envsubst < /var/lib/istio/config/telemetry-sd.yaml > /var/lib/istio/config/data/telemetry.yaml
else
  # Prometheus only.
  envsubst < /var/lib/istio/config/telemetry.yaml > /var/lib/istio/config/data/telemetry.yaml
fi

# Create a tag-specific configmap, including the settings. This is intended for install_asm and tools.

# shellcheck disable=SC2181
if kubectl get -n istio-system cm "env-${REVISION}" ; then
  echo "env file found"
else
  kubectl -n istio-system create cm "env-${REVISION}" \
     --from-literal=CLOUDRUN_ADDR="${CLOUDRUN_ADDR}"
fi


# NB: Local files have .yaml suffix but ConfigMap keys don't
# Local files named after ConfigMap keys shouldn't exist or they'll be used instead of the ConfigMaps
# shellcheck disable=SC2181
if kubectl get -n istio-system cm "istio-${REVISION}"; then
  echo "istio-${REVISION} found"
else
  # Note: we do not override this - user may edit it, in public preview we didn't have config map merging.
  echo "Initializing revision"
  kubectl -n istio-system create cm "istio-${REVISION}" \
     --from-file mesh=/etc/istio/config/mesh.yaml

  # Kube-inject requires the injection files
  # Note that we don't support cluster-side modifications of the files. Istiod
  # will use the local files from the image (config, values).
  # Any customization to injection template will need to have a support policy and
  # use mesh config or other APIs.
  kubectl -n istio-system create cm "istio-sidecar-injector-${REVISION}" \
    --from-file config=/var/lib/istio/inject/config \
    --from-file values=/var/lib/istio/inject/values
fi

# If HUB is enabled, Istiod will run with lower permissions, can't create the webhook. Install tool must create it.
# Eventually all installations should use the install tool/provisioning.
if [[ -z "${GKEHUB}" ]]; then
  # TODO: The script or istioctl should set this up, part of base. This should be removed, so we
  # can drop cluster-admin requirement.
  #
  # Make sure the mutating webhook is installed, and prepare CRDs
  # This also 'warms' up the kubeconfig - otherwise gcloud will slow down startup of istiod.

  # shellcheck disable=SC2181
  if kubectl get mutatingwebhookconfiguration "istiod-${REVISION}" ; then
    echo "Mutating webhook found"
  else
    echo "Mutating webhook missing, initializing"
    envsubst < /var/lib/istio/inject/mutatingwebhook.yaml > /var/lib/istio/inject/mutating.yaml
    cat /var/lib/istio/inject/mutating.yaml
    kubectl apply -f /var/lib/istio/inject/mutating.yaml
  fi
fi

echo Starting "$@"

# What audience to expect for Citadel and XDS - currently using the non-standard format
# TODO: use https://... - and separate token for stackdriver/managedCA
export TOKEN_AUDIENCES="${PROJECT}.svc.id.goog,istio-ca"

# Istiod will report to stackdriver
export ENABLE_STACKDRIVER_MONITORING="${ENABLE_STACKDRIVER_MONITORING:-1}"

export ASM_NODE_ON_FIRST_WORKAROUND=1
export ENABLE_AUTH_DEBUG=1

# For asm-managed, we want this to default on.
export TEE_LOGS_TO_STACKDRIVER=true

# Avoid creating/reading istio-ca-root-cert
export PILOT_CERT_PROVIDER=none

env

# shellcheck disable=SC2068
# shellcheck disable=SC2086
exec /usr/local/bin/pilot-discovery discovery \
   --httpsAddr "" \
   --secureGRPCAddr "" \
   --monitoringAddr "" \
   --grpcAddr "" \
   ${EXTRA_ARGS} ${LOG_ARGS} $@
