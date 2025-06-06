#!/usr/bin/env bash
# shellcheck disable=SC2034

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

# https://stackoverflow.com/questions/59895/how-can-i-get-the-source-directory-of-a-bash-script-from-within-the-script-itsel
# Note: the normal way we use in other scripts in Istio do not work when `source`d, which is why we use this approach
SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
REPO_ROOT="$(dirname "$(dirname "${SCRIPT_DIR}")")"

LOCAL_ARCH=$(uname -m)

# Pass environment set target architecture to build system
if [[ ${TARGET_ARCH} ]]; then
    # Target explicitly set
    :
elif [[ ${LOCAL_ARCH} == x86_64 ]]; then
    TARGET_ARCH=amd64
elif [[ ${LOCAL_ARCH} == armv8* ]]; then
    TARGET_ARCH=arm64
elif [[ ${LOCAL_ARCH} == arm64* ]]; then
    TARGET_ARCH=arm64
elif [[ ${LOCAL_ARCH} == aarch64* ]]; then
    TARGET_ARCH=arm64
elif [[ ${LOCAL_ARCH} == armv* ]]; then
    TARGET_ARCH=arm
elif [[ ${LOCAL_ARCH} == s390x ]]; then
    TARGET_ARCH=s390x
elif [[ ${LOCAL_ARCH} == ppc64le ]]; then
    TARGET_ARCH=ppc64le
else
    echo "This system's architecture, ${LOCAL_ARCH}, isn't supported"
    exit 1
fi

LOCAL_OS=$(uname)

# Pass environment set target operating-system to build system
if [[ ${TARGET_OS} ]]; then
    # Target explicitly set
    :
elif [[ $LOCAL_OS == Linux ]]; then
    TARGET_OS=linux
    readlink_flags="-f"
elif [[ $LOCAL_OS == Darwin ]]; then
    TARGET_OS=darwin
    readlink_flags=""
else
    echo "This system's OS, $LOCAL_OS, isn't supported"
    exit 1
fi

# Build image to use
TOOLS_REGISTRY_PROVIDER=${TOOLS_REGISTRY_PROVIDER:-gcr.io}
PROJECT_ID=${PROJECT_ID:-istio-testing}
if [[ "${IMAGE_VERSION:-}" == "" ]]; then
  IMAGE_VERSION=master-ba21e6d776cfed929785ccdc157d496fbd6567c4
fi
if [[ "${IMAGE_NAME:-}" == "" ]]; then
  IMAGE_NAME=build-tools
fi

DOCKER_GID="${DOCKER_GID:-$(grep '^docker:' /etc/group | cut -f3 -d:)}"

TIMEZONE=$(readlink "$readlink_flags" /etc/localtime | sed -e 's/^.*zoneinfo\///')

TARGET_OUT="${TARGET_OUT:-$(pwd)/out/${TARGET_OS}_${TARGET_ARCH}}"
TARGET_OUT_LINUX="${TARGET_OUT_LINUX:-$(pwd)/out/linux_${TARGET_ARCH}}"

CONTAINER_TARGET_OUT="${CONTAINER_TARGET_OUT:-/work/out/${TARGET_OS}_${TARGET_ARCH}}"
CONTAINER_TARGET_OUT_LINUX="${CONTAINER_TARGET_OUT_LINUX:-/work/out/linux_${TARGET_ARCH}}"

IMG="${IMG:-${TOOLS_REGISTRY_PROVIDER}/${PROJECT_ID}/${IMAGE_NAME}:${IMAGE_VERSION}}"

CONTAINER_CLI="${CONTAINER_CLI:-docker}"

# Try to use the latest cached image we have. Use at your own risk, may have incompatibly-old versions
if [[ "${LATEST_CACHED_IMAGE:-}" != "" ]]; then
  prefix="$(<<<"$IMAGE_VERSION" cut -d- -f1)"
  query="${TOOLS_REGISTRY_PROVIDER}/${PROJECT_ID}/${IMAGE_NAME}:${prefix}-*"
  latest="$("${CONTAINER_CLI}" images --filter=reference="${query}" --format "{{.CreatedAt|json}}~{{.Repository}}:{{.Tag}}~{{.CreatedSince}}" | sort -n -r | head -n1)"
  IMG="$(<<<"$latest" cut -d~ -f2)"
  if [[ "${IMG}" == "" ]]; then
    echo "Attempted to use LATEST_CACHED_IMAGE, but found no images matching ${query}" >&2
    exit 1
  fi
  echo "Using cached image $IMG, created $(<<<"$latest" cut -d~ -f3)" >&2
fi

ENV_BLOCKLIST="${ENV_BLOCKLIST:-^_\|^PATH=\|^GOPATH=\|^GOROOT=\|^SHELL=\|^EDITOR=\|^TMUX=\|^USER=\|^HOME=\|^PWD=\|^TERM=\|^RUBY_\|^GEM_\|^rvm_\|^SSH=\|^TMPDIR=\|^CC=\|^CXX=\|^MAKEFILE_LIST=}"

# Remove functions from the list of exported variables, they mess up with the `env` command.
for f in $(declare -F -x | cut -d ' ' -f 3);
do
  unset -f "${f}"
done

# Set conditional host mounts
CONDITIONAL_HOST_MOUNTS="${CONDITIONAL_HOST_MOUNTS:-} "
container_kubeconfig=''

# docker conditional host mount (needed for make docker push)
if [[ -d "${HOME}/.docker" ]]; then
  CONDITIONAL_HOST_MOUNTS+="--mount type=bind,source=${HOME}/.docker,destination=/config/.docker,readonly "
fi

# gcloud conditional host mount (needed for docker push with the gcloud auth configure-docker)
if [[ -d "${HOME}/.config/gcloud" ]]; then
  CONDITIONAL_HOST_MOUNTS+="--mount type=bind,source=${HOME}/.config/gcloud,destination=/config/.config/gcloud,readonly "
fi

# gitconfig conditional host mount (needed for git commands inside container)
if [[ -f "${HOME}/.gitconfig" ]]; then
  CONDITIONAL_HOST_MOUNTS+="--mount type=bind,source=${HOME}/.gitconfig,destination=/home/.gitconfig,readonly "
fi

# .netrc conditional host mount (needed for git commands inside container)
if [[ -f "${HOME}/.netrc" ]]; then
  CONDITIONAL_HOST_MOUNTS+="--mount type=bind,source=${HOME}/.netrc,destination=/home/.netrc,readonly "
fi

# echo ${CONDITIONAL_HOST_MOUNTS}

# This function checks if the file exists. If it does, it creates a randomly named host location
# for the file, adds it to the host KUBECONFIG, and creates a mount for it. Note that we use a copy
# of the original file, so that the container can write to it.
add_KUBECONFIG_if_exists () {
  if [[ -f "$1" ]]; then
    local local_config
    local_config="$(mktemp)"
    cp "${1}" "${local_config}"

    kubeconfig_random="$(od -vAn -N4 -tx /dev/random | tr -d '[:space:]' | cut -c1-8)"
    container_kubeconfig+="/config/${kubeconfig_random}:"
    CONDITIONAL_HOST_MOUNTS+="--mount type=bind,source=${local_config},destination=/config/${kubeconfig_random} "
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
if [[ "${FOR_BUILD_CONTAINER:-0}" -eq "1" ]]; then
  KUBECONFIG="${container_kubeconfig%?}"
fi

# LOCAL_OUT should point to architecture where we are currently running versus the desired.
# This is used when we need to run a build artifact during tests or later as part of another
# target.
if [[ "${FOR_BUILD_CONTAINER:-0}" -eq "1" ]]; then
  # Override variables with container specific
  TARGET_OUT=${CONTAINER_TARGET_OUT}
  TARGET_OUT_LINUX=${CONTAINER_TARGET_OUT_LINUX}
  REPO_ROOT=/work
  LOCAL_OUT="${TARGET_OUT_LINUX}"
else
  LOCAL_OUT="${TARGET_OUT}"
fi

go_os_arch=${LOCAL_OUT##*/}
# Golang OS/Arch format
LOCAL_GO_OS=${go_os_arch%_*}
LOCAL_GO_ARCH=${go_os_arch##*_}

BUILD_WITH_CONTAINER=0

VARS=(
      CONTAINER_TARGET_OUT
      CONTAINER_TARGET_OUT_LINUX
      TARGET_OUT
      TARGET_OUT_LINUX
      LOCAL_GO_OS
      LOCAL_GO_ARCH
      LOCAL_OUT
      LOCAL_OS
      TARGET_OS
      LOCAL_ARCH
      TARGET_ARCH
      TIMEZONE
      KUBECONFIG
      CONDITIONAL_HOST_MOUNTS
      ENV_BLOCKLIST
      CONTAINER_CLI
      DOCKER_GID
      IMG
      IMAGE_NAME
      IMAGE_VERSION
      REPO_ROOT
      BUILD_WITH_CONTAINER
)

# For non container build, we need to write env to file
if [[ "${1}" == "envfile" ]]; then
  # ! does a variable-variable https://stackoverflow.com/a/10757531/374797
  for var in "${VARS[@]}"; do
    echo "${var}"="${!var}"
  done
else
  for var in "${VARS[@]}"; do
    # shellcheck disable=SC2163
    export "${var}"
  done
fi
