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

set -x # Print out all commands.

if [[ -z ${IN_CLUSTER} ]] ; then
  if [[ -n ${PROJECT} ]]; then

    # Getting the credentials with gcloud currently fails if it's run in a new
    # project for up to the first 7 minutes (but it rarely gets this bad). Cloud
    # Run waits up to 4 minutes for the service to be ready. So retry this
    # command for ~3-4 minutes just in case.
    RET=1
    START=$(date +%s)
    while true; do
      gcloud container clusters get-credentials "${CLUSTER}" --zone "${ZONE}" --project "${PROJECT}" --billing-project "${PROJECT}"
      RET="$?"
      if [[ "${RET}" -eq 0 ]] ; then
        # get-credentials command was successful, exit the loop.
        break
      fi
      # TODO(qfel): Retry only on permission errors.
      if (( $(date +%s) - START > 3 * 60 + 30 )); then
        echo 'gcloud container clusters get-credentials failed'
        exit 1
      fi
      sleep 5
    done

    # TODO: check secret manager for a .kubeconfig - use it for non-GKE projects AND ingress secrets
    # AND citadel root CA
  fi
fi

# Disable webhook config patching - manual configs used, proper DNS certs means no cert patching needed.
# If running in KNative without a DNS cert - we may need it back, but we should require DNS certs.
# Even if user doesn't have a DNS name - they can still use an self-signed root and add it to system trust,
# to simplify
export VALIDATION_WEBHOOK_CONFIG_NAME=
export INJECTION_WEBHOOK_CONFIG_NAME=

# No longer needed.
#export DISABLE_LEADER_ELECTION=true

# No mTLS for control plane
export USE_TOKEN_FOR_CSR=true
export USE_TOKEN_FOR_XDS=true

# Disable the DNS-over-TLS server - no ports
export DNS_ADDR=

# TODO: parse service name and extra project, revision, cluster

export REVISION=${REV:-managed}

# TODO: should be auto-set now, verify safe to remove
export GKE_CLUSTER_URL=https://container.googleapis.com/v1/projects/${PROJECT}/locations/${ZONE}/clusters/${CLUSTER}

export CLUSTER_ID=cn-${PROJECT}-${ZONE}-${CLUSTER}

# Emulate K8S - with one namespace per tenant
export ASM_CONTROL_PLANE_POD_NAMESPACE=${K_CONFIGURATION}

# Check for required arguments
: "${K_REVISION:?K_REVISION not set or is empty}"
# Revision is equivalent with a deployment - unfortunately we can't get instance id.
POD_NAME="${K_REVISION}-$(date +%N)"
export POD_NAME

export ASM_CONTROL_PLANE_POD_NAME=${POD_NAME}

# The auth provider for XDS (e.g., gcp). The default is empty.
export XDS_AUTH_PROVIDER="${XDS_AUTH_PROVIDER:-}"
# The JWT rule for istiod JWT authenticator. The default is empty.
export JWT_RULE="${JWT_RULE:-}"

export XDS_AUTH_PLAINTEXT=true
export XDS_TOKEN_TYPE=${XDS_TOKEN_TYPE:-Bearer}

# Test: see the IP, if unique we can add it to pod name
#ip addr
#hostname

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


if [[ "${CA}" == "1" ]]; then
  export CA_ADDR=meshca.googleapis.com:443
  export TRUST_DOMAIN=${PROJECT}.svc.id.goog
  export AUDIENCE=${TRUST_DOMAIN}
  export CA_PROVIDER=${CA_PROVIDER:-istiod}
  # Disable istiod's ca server because it's unused when we're using MeshCA, and
  # to avoid a race condition where multiple istiods race to create CA secrets
  # at the same time and can potentially fail.
  export ENABLE_CA_SERVER=0
else
  # If not set - the template default is a made-up istiod address instead of discovery.
  # TODO: fix template
  # TODO: if we fetch MeshConfig from cluster - leave trust domain untouched.
  export CA_ADDR=${CLOUDRUN_ADDR}
  export TRUST_DOMAIN=cluster.local
  export AUDIENCE=${PROJECT}.svc.id.goog
  export CA_PROVIDER=istiod
fi

# TODO:
# - copy inject template and mesh config to cluster (first time) or from cluster
# - revision support
# - option to enable 'default' ingress class, remote install/control Gateway

if ! kubectl get ns istio-system; then
  echo "Initializing istio-system and CRDs, fresh cluster"
  kubectl create ns istio-system
  #kubectl apply -k github.com/istio/istio/manifests/charts/base
  kubectl apply -f /var/lib/istio/config/gen-istio-cluster.yaml \
      --record=false --overwrite=false   --force-conflicts=true --server-side
fi
# TODO: check CRD revision, upgrade if needed.

if [[ -n "${MESH}" ]]; then
  echo "${MESH}" > /etc/istio/config/mesh.yaml
else
  envsubst < /etc/istio/config/mesh_template.yaml > /etc/istio/config/mesh.yaml
  cat /etc/istio/config/mesh.yaml
fi

# TODO: fix OSS template to use only MeshConfig !
envsubst < /var/lib/istio/inject/values_template.yaml > /var/lib/istio/inject/values.yaml

# Create file config sources for telemetry
mkdir -p /var/lib/istio/config/data
if [[ "${ASM}" == "1" ]]; then
  envsubst < /var/lib/istio/config/telemetry-sd.yaml > /var/lib/istio/config/data/telemetry.yaml
else
  # Prometheus only.
  envsubst < /var/lib/istio/config/telemetry.yaml > /var/lib/istio/config/data/telemetry.yaml
fi



# NB: Local files have .yaml suffix but ConfigMap keys don't
# Local files named after ConfigMap keys shouldn't exist or they'll be used instead of the ConfigMaps
if ! kubectl get -n istio-system cm "istio-${REVISION}"; then
  echo "Initializing revision"
  kubectl -n istio-system create cm "istio-${REVISION}" --from-file mesh=/etc/istio/config/mesh.yaml
fi


if ! kubectl get -n istio-system cm "istio-sidecar-injector-${REVISION}"; then
  echo "Initializing injector config"
  kubectl -n istio-system create cm "istio-sidecar-injector-${REVISION}" \
    --from-file config=/var/lib/istio/inject/config.yaml \
    --from-file values=/var/lib/istio/inject/values.yaml
fi


# Make sure the mutating webhook is installed, and prepare CRDs
# This also 'warms' up the kubeconfig - otherwise gcloud will slow down startup of istiod.
if kubectl get mutatingwebhookconfiguration "istiod-${REVISION}"; then
  echo "Mutating webhook missing, initializing"
  envsubst < /var/lib/istio/inject/mutating_template.yaml > /var/lib/istio/inject/mutating.yaml
  cat /var/lib/istio/inject/mutating.yaml
  kubectl apply -f /var/lib/istio/inject/mutating.yaml
else
  echo "Mutating webhook found"
fi

echo Starting "$@"

# What audience to expect for Citadel and XDS - currently using the non-standard format
# TODO: use https://... - and separate token for stackdriver/managedCA
export TOKEN_AUDIENCES="${PROJECT}.svc.id.goog,istio-ca"

# Istiod will report to stackdriver
export ENABLE_STACKDRIVER_MONITORING="${ENABLE_STACKDRIVER_MONITORING:-1}"

env

# shellcheck disable=SC2068
# shellcheck disable=SC2086
exec /usr/local/bin/pilot-discovery discovery \
   --httpsAddr "" \
   --trust-domain "${TRUST_DOMAIN}" \
   --secureGRPCAddr "" \
   --monitoringAddr "" \
   --grpcAddr "" \
   ${EXTRA_ARGS} ${LOG_ARGS} $@
