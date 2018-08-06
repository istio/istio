#!/bin/bash
#
# Copyright 2017,2018 Istio Authors. All Rights Reserved.
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

# Init script downloads or updates envoy and the go dependencies. Called from Makefile, which sets
# the needed environment variables.


set -o errexit
set -o nounset
set -o pipefail

# TODO(nmittler): Remove before merging.
set -x # echo on

# TODO(nmittler): Remove these variables and require that this script be run from the Makefile

# Set GOPATH to match the expected layout
GO_TOP=$(cd $(dirname $0)/../../../..; pwd)

export OUT_DIR=${OUT_DIR:-${GO_TOP}/out}

# Current version is 2.9.1, with 2.10RC available
# 2.7.2 was released in Nov 2017.
# 2.10 adds proper support for CRD - we will test with it
# For pre-2.10,
HELM_VER=${HELM_VER:-v2.9.1}
#HELM_VER=${HELM_VER:-v2.10.0-rc.1}

export GOPATH=${GOPATH:-$GO_TOP}
# Normally set by Makefile
export ISTIO_BIN=${ISTIO_BIN:-${GOPATH}/bin}

# Set the architecture. Matches logic in the Makefile.
export GOARCH=${GOARCH:-'amd64'}

# Determine the OS. Matches logic in the Makefile.
LOCAL_OS=${OSTYPE}
case $LOCAL_OS in
  "linux"*)
    LOCAL_OS='linux'
    ;;
  "darwin"*)
    LOCAL_OS='darwin'
    ;;
  *)
    echo "This system's OS ${LOCAL_OS} isn't recognized/supported"
    exit 1
    ;;
esac
export GOOS=${GOOS:-${LOCAL_OS}}

# test scripts seem to like to run this script directly rather than use make
export ISTIO_OUT=${ISTIO_OUT:-${ISTIO_BIN}}

# install helm if not present, it must be the local version.
if [ ! -f ${ISTIO_OUT}/version.helm.${HELM_VER} ] ; then
    TD=$(mktemp -d)
    # Install helm. Please keep it in sync with .circleci
    cd ${TD} && \
        curl -Lo ${TD}/helm.tgz https://storage.googleapis.com/kubernetes-helm/helm-${HELM_VER}-${LOCAL_OS}-amd64.tar.gz && \
        tar xfz helm.tgz && \
        mv ${LOCAL_OS}-amd64/helm ${ISTIO_OUT}/helm-${HELM_VER} && \
        cp ${ISTIO_OUT}/helm-${HELM_VER} ${ISTIO_OUT}/helm && \
        rm -rf ${TD} && \
        touch ${ISTIO_OUT}/version.helm.${HELM_VER}
fi
