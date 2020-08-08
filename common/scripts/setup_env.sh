#!/bin/bash

# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make update-common".

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

set -e

LOCAL_ARCH=$(uname -m)
export LOCAL_ARCH
# Pass environment set target architecture to build system
if [[ ${TARGET_ARCH} ]]; then
    export TARGET_ARCH
elif [[ ${LOCAL_ARCH} == x86_64 ]]; then
    export TARGET_ARCH=amd64
elif [[ ${LOCAL_ARCH} == armv8* ]]; then
    export TARGET_ARCH=arm64
elif [[ ${LOCAL_ARCH} == aarch64* ]]; then
    export TARGET_ARCH=arm64
elif [[ ${LOCAL_ARCH} == armv* ]]; then
    export TARGET_ARCH=arm
else
    echo "This system's architecture, ${LOCAL_ARCH}, isn't supported"
    exit 1
fi

LOCAL_OS=$(uname)
export LOCAL_OS
# Pass environment set target operating-system to build system
if [[ ${TARGET_OS} ]]; then
    export TARGET_OS
elif [[ $LOCAL_OS == Linux ]]; then
    export TARGET_OS=linux
    readlink_flags="-f"
elif [[ $LOCAL_OS == Darwin ]]; then
    export TARGET_OS=darwin
    readlink_flags=""
else
    echo "This system's OS, $LOCAL_OS, isn't supported"
    exit 1
fi

# Build image to use
if [[ "${IMAGE_VERSION:-}" == "" ]]; then
  export IMAGE_VERSION=master-2020-08-07T13-59-49
fi
if [[ "${IMAGE_NAME:-}" == "" ]]; then
  export IMAGE_NAME=build-tools
fi

export UID
DOCKER_GID=$(grep '^docker:' /etc/group | cut -f3 -d:)
export DOCKER_GID

TIMEZONE=$(readlink "$readlink_flags" /etc/localtime | sed -e 's/^.*zoneinfo\///')
export TIMEZONE

export TARGET_OUT="${TARGET_OUT:-$(pwd)/out/${TARGET_OS}_${TARGET_ARCH}}"
export TARGET_OUT_LINUX="${TARGET_OUT_LINUX:-$(pwd)/out/linux_amd64}"

export CONTAINER_TARGET_OUT="${CONTAINER_TARGET_OUT:-/work/out/${TARGET_OS}_${TARGET_ARCH}}"
export CONTAINER_TARGET_OUT_LINUX="${CONTAINER_TARGET_OUT_LINUX:-/work/out/linux_amd64}"

export IMG="${IMG:-gcr.io/istio-testing/${IMAGE_NAME}:${IMAGE_VERSION}}"

export CONTAINER_CLI="${CONTAINER_CLI:-docker}"

export ENV_BLOCKLIST="${ENV_BLOCKLIST:-^_\|PATH\|SHELL\|EDITOR\|TMUX\|USER\|HOME\|PWD\|TERM\|GO\|rvm\|SSH\|TMPDIR\|CC\|CXX\|MAKEFILE_LIST}"

# Remove functions from the list of exported variables, they mess up with the `env` command.
for f in $(declare -F -x | cut -d ' ' -f 3);
do
  unset -f "${f}"
done

# Set conditional host mounts
export CONDITIONAL_HOST_MOUNTS=${CONDITIONAL_HOST_MOUNTS:-}
container_kubeconfig=''

# docker conditional host mount (needed for make docker push)
if [[ -d "${HOME}/.docker" ]]; then
  CONDITIONAL_HOST_MOUNTS+="--mount type=bind,source=${HOME}/.docker,destination=/config/.docker,readonly "
fi

# gcloud conditional host mount (needed for docker push with the gcloud auth configure-docker)
if [[ -d "${HOME}/.config/gcloud" ]]; then
  CONDITIONAL_HOST_MOUNTS+="--mount type=bind,source=${HOME}/.config/gcloud,destination=/config/.config/gcloud,readonly "
fi

# This function checks if the file exists. If it does, it creates a randomly named host location
# for the file, adds it to the host KUBECONFIG, and creates a mount for it.
add_KUBECONFIG_if_exists () {
  if [[ -f "$1" ]]; then
    kubeconfig_random="$(od -vAn -N4 -tx /dev/random | tr -d '[:space:]' | cut -c1-8)"
    container_kubeconfig+="/home/${kubeconfig_random}:"
    CONDITIONAL_HOST_MOUNTS+="--mount type=bind,source=${1},destination=/config/${kubeconfig_random},readonly "
  fi
}

# This function is designed for maximum compatibility with various platforms. This runs on
# any Mac or Linux platform with bash 4.2+. Please take care not to modify this function
# without testing properly.
#
# This function will properly handle any type of path including those with spaces using the
# loading pattern specified by *kubectl config*.
#
# testcase: "a:b c:d"
# testcase: "a b:c d:e f"
# testcase: "a b:c:d e"
parse_KUBECONFIG () {
TMPDIR=""
if [[ "$1" =~ ([^:]*):(.*) ]]; then
  while true; do
    rematch=${BASH_REMATCH[1]}
    add_KUBECONFIG_if_exists "$rematch"
    remainder="${BASH_REMATCH[2]}"
    if [[ ! "$remainder" =~ ([^:]*):(.*) ]]; then
      if [[ -n "$remainder" ]]; then
        add_KUBECONFIG_if_exists "$remainder"
        break
      fi
    fi
  done
else
  add_KUBECONFIG_if_exists "$1"
fi
}

KUBECONFIG=${KUBECONFIG:="$HOME/.kube/config"}
parse_KUBECONFIG "${KUBECONFIG}"
if [[ "${BUILD_WITH_CONTAINER:-1}" -eq "1" ]]; then
  export KUBECONFIG="${container_kubeconfig%?}"
fi

# Avoid recursive calls to make from attempting to start an additional container
export BUILD_WITH_CONTAINER=0

# For non container build, we need to write env to file
if [[ "${1}" == "envfile" ]]; then
  echo "TARGET_OUT_LINUX=${TARGET_OUT_LINUX}"
  echo "TARGET_OUT=${TARGET_OUT}"
  echo "TIMEZONE=${TIMEZONE}"
  echo "LOCAL_OS=${LOCAL_OS}"
  echo "TARGET_OS=${TARGET_OS}"
  echo "LOCAL_ARCH=${LOCAL_ARCH}"
  echo "TARGET_ARCH=${TARGET_ARCH}"
  echo "BUILD_WITH_CONTAINER=0"
fi
