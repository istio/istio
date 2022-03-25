#!/bin/bash

# Copyright 2018 Istio Authors
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

# Init script downloads or updates envoy and the go dependencies. Called from Makefile, which sets
# the needed environment variables.

set -o errexit
set -o nounset
set -o pipefail

if [[ "${ISTIO_ENVOY_LINUX_RELEASE_URL:-}" == "" ]]; then
  echo "Envoy variables no set. Make sure you run through the makefile (\`make init\`) rather than directly."
  exit 1
fi

# Download Envoy debug and release binaries for Linux x86_64. They will be included in the
# docker images created by Dockerfile.proxyv2.

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

# Downloads and extract an Envoy binary if the artifact doesn't already exist.
# Params:
#   $1: The URL of the Envoy tar.gz to be downloaded.
#   $2: The full path of the output binary.
#   $3: Non-versioned name to use
function download_envoy_if_necessary () {
  if [[ ! -f "$2" ]] ; then
    # Enter the output directory.
    mkdir -p "$(dirname "$2")"
    pushd "$(dirname "$2")"

    # Download and extract the binary to the output directory.
    echo "Downloading ${SIDECAR}: $1 to $2"
    time ${DOWNLOAD_COMMAND} --header "${AUTH_HEADER:-}" "$1" | tar xz

    # Copy the extracted binary to the output location
    cp usr/local/bin/"${SIDECAR}" "$2"

    # Remove the extracted binary.
    rm -rf usr

    # Make a copy named just "envoy" in the same directory (overwrite if necessary).
    echo "Copying $2 to $(dirname "$2")/${3}"
    cp -f "$2" "$(dirname "$2")/${3}"
    popd
  fi
}

# Downloads WebAssembly based plugin if it doesn't already exist.
# Params:
#   $1: The URL of the WebAssembly file to be downloaded.
#   $2: The full path of the output file.
function download_wasm_if_necessary () {
  download_file_dir="$(dirname "$2")"
  download_file_name="$(basename "$1")"
  download_file_path="${download_file_dir}/${download_file_name}"
  if [[ ! -f "${download_file_path}" ]] ; then
    # Enter the output directory.
    mkdir -p "${download_file_dir}"
    pushd "${download_file_dir}"

    # Download the WebAssembly plugin files to the output directory.
    echo "Downloading WebAssembly file: $1 to ${download_file_path}"
    if [[ ${DOWNLOAD_COMMAND} == curl* ]]; then
      time ${DOWNLOAD_COMMAND} --header "${AUTH_HEADER:-}" "$1" -o "${download_file_name}"
    elif [[ ${DOWNLOAD_COMMAND} == wget* ]]; then
      time ${DOWNLOAD_COMMAND} --header "${AUTH_HEADER:-}" "$1" -O "${download_file_name}"
    fi

    # Copy the webassembly file to the output location
    cp "${download_file_path}" "$2"
    popd
  fi
}

mkdir -p "${TARGET_OUT}"

# Set the value of DOWNLOAD_COMMAND (either curl or wget)
set_download_command

if [[ -n "${DEBUG_IMAGE:-}" ]]; then
  # Download and extract the Envoy linux debug binary.
  download_envoy_if_necessary "${ISTIO_ENVOY_LINUX_DEBUG_URL}" "$ISTIO_ENVOY_LINUX_DEBUG_PATH" "${SIDECAR}"
else
  echo "Skipping envoy debug. Set DEBUG_IMAGE to download."
fi

# Download and extract the Envoy linux release binary.
download_envoy_if_necessary "${ISTIO_ENVOY_LINUX_RELEASE_URL}" "$ISTIO_ENVOY_LINUX_RELEASE_PATH" "${SIDECAR}"
download_envoy_if_necessary "${ISTIO_ENVOY_CENTOS_RELEASE_URL}" "$ISTIO_ENVOY_CENTOS_LINUX_RELEASE_PATH" "${SIDECAR}-centos"

if [[ "$GOOS_LOCAL" == "darwin" ]]; then
  # Download and extract the Envoy macOS release binary
  download_envoy_if_necessary "${ISTIO_ENVOY_MACOS_RELEASE_URL}" "$ISTIO_ENVOY_MACOS_RELEASE_PATH" "${SIDECAR}"
  ISTIO_ENVOY_NATIVE_PATH=${ISTIO_ENVOY_MACOS_RELEASE_PATH}
else
  ISTIO_ENVOY_NATIVE_PATH=${ISTIO_ENVOY_LINUX_RELEASE_PATH}
fi

# Download WebAssembly plugin files
WASM_RELEASE_DIR=${ISTIO_ENVOY_LINUX_RELEASE_DIR}
for plugin in stats metadata_exchange
do
  FILTER_WASM_URL="${ISTIO_ENVOY_BASE_URL}/${plugin}-${ISTIO_ENVOY_VERSION}.wasm"
  download_wasm_if_necessary "${FILTER_WASM_URL}" "${WASM_RELEASE_DIR}"/"${plugin//_/-}"-filter.wasm
  FILTER_WASM_URL="${ISTIO_ENVOY_BASE_URL}/${plugin}-${ISTIO_ENVOY_VERSION}.compiled.wasm"
  download_wasm_if_necessary "${FILTER_WASM_URL}" "${WASM_RELEASE_DIR}"/"${plugin//_/-}"-filter.compiled.wasm
done

# Copy native envoy binary to TARGET_OUT
echo "Copying ${ISTIO_ENVOY_NATIVE_PATH} to ${TARGET_OUT}/${SIDECAR}"
cp -f "${ISTIO_ENVOY_NATIVE_PATH}" "${TARGET_OUT}/${SIDECAR}"

# Copy CentOS binary
echo "Copying ${ISTIO_ENVOY_CENTOS_LINUX_RELEASE_PATH} to ${TARGET_OUT_LINUX}/${SIDECAR}-centos"
cp -f "${ISTIO_ENVOY_CENTOS_LINUX_RELEASE_PATH}" "${TARGET_OUT_LINUX}/${SIDECAR}-centos"

# Copy the envoy binary to TARGET_OUT_LINUX if the local OS is not Linux
if [[ "$GOOS_LOCAL" != "linux" ]]; then
   echo "Copying ${ISTIO_ENVOY_LINUX_RELEASE_PATH} to ${TARGET_OUT_LINUX}/${SIDECAR}"
  cp -f "${ISTIO_ENVOY_LINUX_RELEASE_PATH}" "${TARGET_OUT_LINUX}/${SIDECAR}"
fi
