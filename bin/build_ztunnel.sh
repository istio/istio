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

set -o errexit
set -o nounset
set -o pipefail

if [[ "${TARGET_OUT_LINUX:-}" == "" ]]; then
  echo "Environment variables not set. Make sure you run through the makefile (\`make init\`) rather than directly."
  exit 1
fi

# Gets the download command supported by the system (currently either curl or wget)
DOWNLOAD_COMMAND=""
function set_download_command () {
  # Try curl.
  if command -v curl > /dev/null; then
    if curl --version | grep Protocols  | grep https > /dev/null; then
      DOWNLOAD_COMMAND="curl -fLSs --retry 5 --retry-delay 1 --retry-connrefused"
      return
    fi
    echo curl does not support https, will try wget for downloading files.
  else
    echo curl is not installed, will try wget for downloading files.
  fi

  # Try wget.
  if command -v wget > /dev/null; then
    DOWNLOAD_COMMAND="wget -qO -"
    return
  fi
  echo wget is not installed.

  echo Error: curl is not installed or does not support https, wget is not installed. \
       Cannot download envoy. Please install wget or add support of https to curl.
  exit 1
}

# Params:
#   $1: The URL of the ztunnel binary to be downloaded.
#   $2: The full path of the output binary.
#   $3: Non-versioned name to use
function download_ztunnel_if_necessary () {
  if [[ -f "$2" ]]; then
    return
  fi
  # Enter the output directory.
  mkdir -p "$(dirname "$2")"
  pushd "$(dirname "$2")"

  # Download and make the binary executable
  echo "Downloading ztunnel: $1 to $2"
  time ${DOWNLOAD_COMMAND} --header "${AUTH_HEADER:-}" "$1" > "$2"
  chmod +x "$2"

  # Make a copy named just "ztunnel" in the same directory (overwrite if necessary).
  echo "Copying $2 to $(dirname "$2")/${3}"
  cp -f "$2" "$(dirname "$2")/${3}"
  popd

  # Also copy it to out/$os_arch/ztunnel as that's what's used in the build
  echo "Copying '${2}' to ${TARGET_OUT_LINUX}/ztunnel"
  cp -f "${2}" "${TARGET_OUT_LINUX}/ztunnel"
}

function maybe_build_ztunnel() {
  # TODO detect git changes or something to avoid unnecessarily building
  # BUILD_ZTUNNEL=1 with no BUILD_ZTUNNEL_REPO tries to infer BUILD_ZTUNNEL_REPO
  if [[ "${BUILD_ZTUNNEL_REPO:-}" == "" ]] && [[ "${BUILD_ZTUNNEL:-}" != "" ]]; then
    local ZTUNNEL_DIR
	ZTUNNEL_DIR="$(pwd)/../ztunnel"
    if [[ -d "${ZTUNNEL_DIR}" ]]; then
      BUILD_ZTUNNEL_REPO="${ZTUNNEL_DIR}"
    else
      echo "No directory at ${ZTUNNEL_DIR}"
      return
    fi
  fi
  if [[ "${BUILD_ZTUNNEL_REPO:-}" == "" ]]; then
    return
  fi

  if ! which cargo; then
    echo "the rust toolchain (cargo, etc) is required for building ztunnel"
    return 1
  fi

  pushd "${BUILD_ZTUNNEL_REPO}"
  cargo build --profile="${BUILD_ZTUNNEL_PROFILE:-dev}" ${BUILD_ZTUNNEL_TARGET:+--target=${BUILD_ZTUNNEL_TARGET}}

  local ZTUNNEL_BIN_PATH
  if [[ "${BUILD_ZTUNNEL_PROFILE:-dev}" == "dev" ]]; then
      ZTUNNEL_BIN_PATH=debug
  else
      ZTUNNEL_BIN_PATH="${BUILD_ZTUNNEL_PROFILE}"
  fi
  ZTUNNEL_BIN_PATH="out/rust/${BUILD_ZTUNNEL_TARGET:+${BUILD_ZTUNNEL_TARGET}/}${ZTUNNEL_BIN_PATH}/ztunnel"

  echo "Copying $(pwd)/${ZTUNNEL_BIN_PATH} to ${TARGET_OUT_LINUX}/ztunnel"
  mkdir -p "${TARGET_OUT_LINUX}"
  cp "${ZTUNNEL_BIN_PATH}" "${TARGET_OUT_LINUX}/ztunnel"
  popd
}

# ztunnel binary vars (TODO handle debug builds, arm, darwin etc.)
ISTIO_ZTUNNEL_BASE_URL="${ISTIO_ZTUNNEL_BASE_URL:-https://storage.googleapis.com/istio-build/ztunnel}"

# If we are not using the default, assume its private and we need to authenticate
if [[ "${ISTIO_ZTUNNEL_BASE_URL}" != "https://storage.googleapis.com/istio-build/ztunnel" ]]; then
  AUTH_HEADER="Authorization: Bearer $(gcloud auth print-access-token)"
  export AUTH_HEADER
fi

ZTUNNEL_REPO_SHA="${ZTUNNEL_REPO_SHA:-$(grep ZTUNNEL_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')}"
ISTIO_ZTUNNEL_VERSION="${ISTIO_ZTUNNEL_VERSION:-${ZTUNNEL_REPO_SHA}}"
ISTIO_ZTUNNEL_RELEASE_URL="${ISTIO_ZTUNNEL_RELEASE_URL:-${ISTIO_ZTUNNEL_BASE_URL}/ztunnel-${ISTIO_ZTUNNEL_VERSION}-${TARGET_ARCH}}"
ISTIO_ZTUNNEL_LINUX_RELEASE_NAME="${ISTIO_ZTUNNEL_LINUX_RELEASE_NAME:-ztunnel-${ISTIO_ZTUNNEL_VERSION}}"
ISTIO_ZTUNNEL_LINUX_RELEASE_DIR="${ISTIO_ZTUNNEL_LINUX_RELEASE_DIR:-${TARGET_OUT_LINUX}/release}"
ISTIO_ZTUNNEL_LINUX_DEBUG_DIR="${ISTIO_ZTUNNEL_LINUX_DEBUG_DIR:-${TARGET_OUT_LINUX}/debug}"
ISTIO_ZTUNNEL_LINUX_RELEASE_PATH="${ISTIO_ZTUNNEL_LINUX_RELEASE_PATH:-${ISTIO_ZTUNNEL_LINUX_RELEASE_DIR}/${ISTIO_ZTUNNEL_LINUX_RELEASE_NAME}}"

set_download_command
maybe_build_ztunnel
download_ztunnel_if_necessary "${ISTIO_ZTUNNEL_RELEASE_URL}" "$ISTIO_ZTUNNEL_LINUX_RELEASE_PATH" "ztunnel"
