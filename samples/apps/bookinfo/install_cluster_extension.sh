#!/bin/bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
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
################################################################################

# Script to configure an external machine for bookinfo demo.

ISTIO_BASE=${ISTIO_BASE:-${GOPATH:-${HOME}/go}}
ISTIO_IO=${ISTIO_IO:-${ISTIO_BASE}/src/istio.io}

. $ISTIO_IO/istio/install/tools/istio_vm_common.sh

VM=${VM:-vmextension}

function installFiles() {
    local VM=$1
    # This only needs to be run once per cluster - but no harm in running it again.
    istioInitILB
    istioGenerateClusterConfigs

    # TODO: download files from release build
    # Copy from build until release files are out - will populate ISTIO_FILES with the .deb and helper files.
    istioCopyBuildFiles

    # Prepare the VM, run istio on the VM
    istioBootstrapVM $VM

    # Copy the details app to the VM
    istioCopy $VM \
      $ISTIO_IO/istio/samples/apps/bookinfo/src/details \
      $ISTIO_IO/istio/samples/apps/bookinfo/cluster_extension.sh

    istioRun $VM "./cluster_extension.sh install"


}

# Helper to create the raw VM. You can also use an existing VM, or create using the UI.
# Currently uses gcloud - can be extended with other environments.
function istioVMInit() {
  # TODO: check if it exists and do "reset", to speed up the script.
  local NAME=$1
  local IMAGE=${2:-debian-9-stretch-v20170816}
  local IMAGE_PROJECT=${3:-debian-cloud}

  gcloud compute --project $PROJECT instances  describe $NAME  --zone ${ISTIO_ZONE} >/dev/null
  if [[ $? == 0 ]] ; then

    gcloud compute --project $PROJECT \
     instances reset $NAME \
     --zone $ISTIO_ZONE \

  else

    gcloud compute --project $PROJECT \
     instances create $NAME \
     --zone $ISTIO_ZONE \
     --machine-type "n1-standard-1" \
     --subnet default \
     --can-ip-forward \
     --service-account $ACCOUNT \
     --scopes "https://www.googleapis.com/auth/cloud-platform" \
     --tags "http-server","https-server" \
     --image $IMAGE \
     --image-project $IMAGE_PROJECT \
     --boot-disk-size "10" \
     --boot-disk-type "pd-standard" \
     --boot-disk-device-name "debtest"

  fi

  # Allow access to the VM on port 80 and 9411 (where we run services)
  gcloud compute firewall-rules create allow-external  --allow tcp:22,tcp:80,tcp:443,tcp:9411,udp:5228,icmp  --source-ranges 0.0.0.0/0

  # Wait for machine to start up ssh
  for i in {1..10}
  do
    istioRun $NAME 'echo hi'
    if [[ $? -ne 0 ]] ; then
        echo Waiting for startup $?
        sleep 5
    else
        break
    fi
  done

}

if [[ -z "$VM" || -z "$K8S_CLUSTER" ]] ; then
  echo "Environment variables used to run the test:"
  echo
  echo "VM - name of the virtual machine, to use in ssh and scp commands"
  echo "K8S_CLUSTER - name of the k8s cluster"

else
  shift
  installFiles $VM
fi
