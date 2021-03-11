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


# retries the given command N times, terminating and retrying after a given time period.
# There is a 2 second backoff between attempts.
# Parameters: $1 - Max attempts
#             $2 - Time period to allow command to run for
#             $@ - Remaining arguments make up the command to run.
retry() {
  local MAX_TRIES="${1}"; shift 1
  local TIMEOUT="${1}"; shift 1

  for i in $(seq 0 "${MAX_TRIES}"); do
    if [[ "${i}" -eq "${MAX_TRIES}" ]]; then
      false
      return
    fi
    if [ -n "${TIMEOUT}" ]; then
      { timeout "${TIMEOUT}" "${@}" && return 0; } || true
    else
      { "${@}" && return 0; } || true
    fi
    echo "Failed, retrying...($((i+1)) of ${MAX_TRIES})"
    sleep 2
  done
  false
}

TOPOLOGY_JQ_PROG='.[] | "    - service: \(.labels.service)
      namespace: \(.labels.namespace)
      instances:
      - ip: \(.networkInterfaces[0].accessConfigs[0].natIP)
        instanceIP: \(.networkInterfaces[0].networkIP)"'

# finds the public IP of the test runner which can be used to create firewall rules
function runner_public_ip() {
  if which dig; then
    dig TXT +short o-o.myaddr.l.google.com @ns1.google.com | awk -F'"' '{ print $2}'
    return 0
  fi

  if res=$(curl ifconfig.me); then
    echo "${res}"
    return 0
  fi

  return 1
}

# Allow traffic from this Prow node to all VMs tagged `gcevm` in the PROJECT_ID where ASM/VMs live.
 # Parameters: $1 - Project ID where the VMs reside
function firewall_rule() {
  gcloud compute firewall-rules create \
    --project "$PROJECT_ID" \
    --allow=tcp:22,tcp:7070,tcp:17070 \
    --source-ranges="$(runner_public_ip)/32" \
    --target-tags=prow-test-vm \
    prow-to-static-vms
}

# Outputs YAML to the given file, in the structure of []cluster.Config to inform the test framework of details about
# static VMs running the test app. cluster.Config is defined in pkg/test/framework/components/cluster/factory.go.
# Parameters: $1 - path to the file to append to
#             $2 - k8s context of the cluster VMs are connected to
function static_vm_topology_entry() {
  local FILE="$1"
  local PRIMARY_CLUSTER_CONTEXT="$2"
  IFS="_" read -r -a VALS <<< "${PRIMARY_CLUSTER_CONTEXT}"
  local PROJECT_ID="${VALS[1]}"
  local LOCATION="${VALS[2]}"
  local CLUSTER_NAME="${VALS[3]}"

  cat << EOF >> "${FILE}"
- kind: StaticVM
  clusterName: static-vms
  primaryClusterName: "cn-${PROJECT_ID}-${LOCATION}-${CLUSTER_NAME}"
  meta:
    deployments:
EOF
  gcloud compute instances list \
    --format json \
    --filter="tags:staticvm" \
    --project="${PROJECT_ID}" \
      | jq -r "${TOPOLOGY_JQ_PROG}" >> "${FILE}"
}

function setup_gce_vms() {
  local FILE="$1"
  local CONTEXT="$2"
  IFS="_" read -r -a VALS <<< "${CONTEXT}"
  local PROJECT_ID="${VALS[1]}"
  local LOCATION="${VALS[2]}"
  local CLUSTER_NAME="${VALS[3]}"
  local REVISION
  REVISION="$(kubectl --context="${CONTEXT}" -n istio-system get service istio-eastwestgateway -ojsonpath='{.metadata.labels.istio\.io/rev}')"

  local PROJECT_NUMBER
  PROJECT_NUMBER=$(gcloud projects describe "${PROJECT_ID}" --format="value(projectNumber)")
  firewall_rule "${PROJECT_ID}"

  cat << EOF >> "${FILE}"
- kind: ASMVM
  clusterName: asm-vms
  primaryClusterName: "cn-${PROJECT_ID}-${LOCATION}-${CLUSTER_NAME}"
  meta:
    project: ${PROJECT_ID}
    projectNumber: ${PROJECT_NUMBER}
    gkeLocation: ${LOCATION}
    gkeCluster: ${CLUSTER_NAME}
    asm_vm: ${VM_SCRIPT}
    firewallTag: prow-test-vm
    env:
    - key: ASM_REVISION_PREFIX
      value: ${REVISION}
    - key: SERVICE_PROXY_AGENT_BUCKET
      value: ${AGENT_BUCKET}
    - key: _CI_ASM_IMAGE_TAG
      value: ${TAG}
EOF
}

# Create virtual machines connected to an ASM cluster.
# Parameters: $1 - CONTEXT: Kube context to use for creating WorkloadGroups and namespaces
#             $2 - CLUSTER_NAME: GKE cluster to connect the VM to.
#             $3 - LOCATION: GCP zone to deploy the VM to.
#             $4 - PROJECT_NUMBER: GCP project to deploy the VM to.
#             $5 - REVISION: ASM revision to create a namespace with.
#             $6 - DIR: A directory with a subdirectory for each VM to create.
#                  These subdirectories should have a workloadgroup.yaml and echo.service file.
# Depends on the env vars: VM_SCRIPT (path to asm_vm), ISTIO_OUT (path to compilation output)
function setup_static_vms() {
  local CONTEXT="${1}"
  local CLUSTER_NAME="${2}"
  local LOCATION="${3}"
  local PROJECT_NUMBER="${4}"
  local REVISION="${5}"
  local DIR="${6}"
  local VM_DISTRO="${7:-debian-10}"
  local IMAGE_PROJECT="${8:-debian-cloud}"

  if [ ! -d "$DIR" ]; then
    echo "No directory $DIR"
    return 1
  fi

  firewall_rule "${PROJECT_ID}"

  for subdir in "$DIR"/*; do
    if [ ! -d "$subdir" ]; then
      continue
    fi
    setup_vm "${CONTEXT}" "${CLUSTER_NAME}" "${LOCATION}" "${PROJECT_NUMBER}" "${REVISION}" "${subdir}" "${VM_DISTRO}" "${IMAGE_PROJECT}"
  done
}

# Create a virtual machine connected to an ASM cluster.
# and assumes indentation is 2 spaces.
# Parameters: $1 - CONTEXT: Kube context to use for creating WorkloadGroups and namespaces
#             $2 - CLUSTER_NAME: GKE cluster to connect the VM to.
#             $3 - LOCATION: GCP zone to deploy the VM to.
#             $4 - PROJECT_NUMBER: GCP project to deploy the VM to.
#             $5 - REVISION: ASM revision to create a namespace with.
#             $6 - DIR: A directory containing the needed config files:
#
#             workloadgroup.yaml - A valid WorkloadGroup with name, namespace specified.
#             The service account is automatically populated.
#
#             echo.service - A systemd unit file that contains the startup args for echo.
# Depends on the env vars: VM_SCRIPT (path to asm_vm), ISTIO_OUT (path to compilation output)
function setup_vm() {
  local CONTEXT="${1}"
  local CLUSTER_NAME="${2}"
  local LOCATION="${3}"
  local PROJECT_NUMBER="${4}"
  local REVISION="${5}"
  local DIR="${6}"
  local VM_DISTRO="${7:-debian-10}"
  local IMAGE_PROJECT="${8:-debian-cloud}"

  # compute instances need a fully qualified zone
  ZONE="${LOCATION}"
  if [[ ! "${ZONE}" =~ [a-z]+-[a-z]+[0-9]-[a-z] ]]; then
    if [[ "${ZONE}" =~ [a-z]+-[a-z]+[0-9] ]]; then
      echo "appending '-a' to ${ZONE} to make a valid zone for VMs"
      ZONE="${ZONE}-a"
    else
      echo "warning: location ${ZONE}} seems invalid"
    fi
  fi

  local ECHO_APP="$ISTIO_OUT/linux_amd64/server"
  if [ ! -f "$ECHO_APP" ]; then
    echo "No file $ECHO_APP. Run prepare_images before setting up VMs."
    return 1
  fi

  for FILE in "workloadgroup.yaml" "echo.service"; do
    [ ! -f "$DIR/$FILE" ] && echo "No file $DIR/$FILE." && return 1
  done

  local NAME
  NAME=$(yq r "$DIR/workloadgroup.yaml" "metadata.name")
  local NAMESPACE
  NAMESPACE=$(yq r "$DIR/workloadgroup.yaml" "metadata.namespace")
  local SERVICE
  SERVICE=$(yq r "$DIR/workloadgroup.yaml" "spec.metadata.labels.app")

  # Create the namespace and push the WorkloadGroup
  kubectl create namespace "${NAMESPACE}" --dry-run -o yaml --context="${CONTEXT}" | kubectl apply -f - --context="${CONTEXT}"
  if [ "${REVISION}" == "default" ]; then
    kubectl --context="${CONTEXT}" label ns "${NAMESPACE}" "istio-injection=enabled" --overwrite
  fi
  kubectl --context="${CONTEXT}" label ns "${NAMESPACE}" "istio.io/rev=${REVISION}" --overwrite
  kubectl --context="${CONTEXT}" apply -f "$DIR/workloadgroup.yaml"
  kubectl --context="${CONTEXT}" -n "$NAMESPACE" patch wg "${NAME}" --type merge --patch "$(cat <<EOF
spec:
  template:
    serviceAccount: "${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
EOF
)"

  # create template and instance
  # some tests expect the hostname to begin with "${NAME}-"
  local INSTANCE_NAME="${NAME}-${NAMESPACE}-${CLUSTER_NAME}-test-app-instance"
  local TEMPLATE_NAME="${NAME}-${NAMESPACE}-${CLUSTER_NAME}-test-app-template"
  local BASE_INSTANCE_TEMPLATE_NAME="${NAME}-${NAMESPACE}-${CLUSTER_NAME}-source-instance-template"
  local TEMPLATE_EXISTS
  local INSTANCE_EXISTS
  TEMPLATE_EXISTS="$(gcloud compute instance-templates describe "${TEMPLATE_NAME}" || true)"
  INSTANCE_EXISTS="$(gcloud compute instances describe --zone="${ZONE}" "${INSTANCE_NAME}" || true)"

  retry 3 10s gcloud compute instance-templates create "${BASE_INSTANCE_TEMPLATE_NAME}" --image-project "${IMAGE_PROJECT}" --image-family "${VM_DISTRO}"
  # eventually this will be a static URL - for the time being this needs to be updated to use the latest agent
  [ -z "$TEMPLATE_EXISTS" ] && ASM_REVISION_PREFIX="${REVISION}" _CI_ASM_IMAGE_TAG="${TAG}" SERVICE_PROXY_AGENT_BUCKET="${AGENT_BUCKET}" $VM_SCRIPT create_gce_instance_template \
    "${TEMPLATE_NAME}" \
    -v \
    --project_id "${PROJECT_ID}" \
    --cluster_name "${CLUSTER_NAME}" \
    --cluster_location "${LOCATION}" \
    --workload_name "${NAME}" \
    --workload_namespace "${NAMESPACE}" \
    --source_instance_template "${BASE_INSTANCE_TEMPLATE_NAME}"
  [ -z "$INSTANCE_EXISTS" ] && gcloud compute --project="${PROJECT_ID}" instances create "${INSTANCE_NAME}" --zone="${ZONE}" \
    --labels="service=${SERVICE},namespace=${NAMESPACE}" --tags="staticvm" \
    --source-instance-template "${TEMPLATE_NAME}"

  # copy echo to the cluster
  retry 6 10s gcloud compute scp "${ECHO_APP}" staticvm@"${INSTANCE_NAME}":~ --zone="${ZONE}"
  retry 6 10s gcloud compute scp "${DIR}/echo.service" staticvm@"${INSTANCE_NAME}":~ --zone="${ZONE}"

  # install echo as a systemd controlled service and run it
  local INTERNAL_IP
  INTERNAL_IP=$(gcloud compute instances describe --zone="${ZONE}" "${INSTANCE_NAME}" --format="get(networkInterfaces[0].networkIP)")
  retry 6 15s gcloud compute ssh staticvm@"${INSTANCE_NAME}" --zone="${ZONE}" --command "$(cat <<EOF
sudo mv ~/server /usr/sbin/echo
sudo mv ~/echo.service /etc/systemd/system/echo.service
which restorecon && sudo restorecon /etc/systemd/system/echo.service
echo INSTANCE_IP=$INTERNAL_IP >> .echoconfig
echo CLUSTER_ID=static-vms >> .echoconfig
sudo mv .echoconfig /etc/.echoconfig
which restorecon && sudo restorecon /etc/.echoconfig
sudo chmod +x /usr/sbin/echo
sudo chmod +r /etc/.echoconfig
which restorecon && sudo restorecon /usr/sbin/echo
sudo systemctl daemon-reload
sudo systemctl restart echo.service
sudo systemctl enable echo.service
EOF
)"

}
