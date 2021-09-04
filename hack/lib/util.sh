#!/usr/bin/env bash

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

# This file is copy form kubernetes.
ISTIO::util::host_os() {
  local host_os
  case "$(uname -s)" in
    Darwin)
      host_os=darwin
      ;;
    Linux)
      host_os=linux
      ;;
    *)
      ISTIO::log::error "Unsupported host OS.  Must be Linux or Mac OS X."
      exit 1
      ;;
  esac
  echo "${host_os}"
}

ISTIO::util::host_arch() {
  local host_arch
  case "$(uname -m)" in
    x86_64*)
      host_arch=amd64
      ;;
    i?86_64*)
      host_arch=amd64
      ;;
    amd64*)
      host_arch=amd64
      ;;
    aarch64*)
      host_arch=arm64
      ;;
    arm64*)
      host_arch=arm64
      ;;
    arm*)
      host_arch=arm
      ;;
    i?86*)
      host_arch=x86
      ;;
    s390x*)
      host_arch=s390x
      ;;
    ppc64le*)
      host_arch=ppc64le
      ;;
    *)
      ISTIO::log::error "Unsupported host arch. Must be x86_64, 386, arm, arm64, s390x or ppc64le."
      exit 1
      ;;
  esac
  echo "${host_arch}"
}

# This figures out the host platform without relying on golang.  We need this as
# we don't want a golang install to be a prerequisite to building yet we need
# this info to figure out where the final binaries are placed.
ISTIO::util::host_platform() {
  echo "$(ISTIO::util::host_os)/$(ISTIO::util::host_arch)"
}

# looks for $1 in well-known output locations for the platform ($2)
# $ISTIO_ROOT must be set
ISTIO::util::find-binary-for-platform() {
  local -r lookfor="$1"
  local -r platform="$2"
  local locations=(
    "${ISTIO_ROOT}/_output/bin/${lookfor}"
    "${ISTIO_ROOT}/_output/dockerized/bin/${platform}/${lookfor}"
    "${ISTIO_ROOT}/_output/local/bin/${platform}/${lookfor}"
    "${ISTIO_ROOT}/platforms/${platform}/${lookfor}"
  )
  # if we're looking for the host platform, add local non-platform-qualified search paths
  if [[ "${platform}" = "$(ISTIO::util::host_platform)" ]]; then
    locations+=(
      "${ISTIO_ROOT}/_output/local/go/bin/${lookfor}"
      "${ISTIO_ROOT}/_output/dockerized/go/bin/${lookfor}"
    );
  fi

  # List most recently-updated location.
  local -r bin=$( (ls -t "${locations[@]}" 2>/dev/null || true) | head -1 )

  if [[ -z "${bin}" ]]; then
    ISTIO::log::error "Failed to find binary ${lookfor} for platform ${platform}"
    return 1
  fi

  echo -n "${bin}"
}

# looks for $1 in well-known output locations for the host platform
# $ISTIO_ROOT must be set
ISTIO::util::find-binary() {
  ISTIO::util::find-binary-for-platform "$1" "$(ISTIO::util::host_platform)"
}
