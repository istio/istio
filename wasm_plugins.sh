#!/bin/bash

# Copyright 2019 Istio Authors
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

# Istio Wasm Plugins Script download Stats and Metadata Exchange Plugins
# and stores them in $OUTDIR/wasm directory.

ROOTDIR=$(cd "$(dirname "$0")"/..; pwd)

OUTDIR=${1?Error: no output dir given}


# Gets the download command supported by the system (currently either curl or wget)
DOWNLOAD_COMMAND=""
function set_download_command () {
  # Try curl.
  if command -v curl > /dev/null; then
    if curl --version | grep Protocols  | grep https > /dev/null; then
      DOWNLOAD_COMMAND='curl -fLSs'
      return
    fi
    echo curl does not support https, will try wget for downloading files.
  else
    echo curl is not installed, will try wget for downloading files.
  fi

  # Try wget.
  if command -v wget > /dev/null; then
    DOWNLOAD_COMMAND='wget -qO -'
    return
  fi
  echo wget is not installed.

  echo Error: curl is not installed or does not support https, wget is not installed. \
       Cannot download envoy. Please install wget or add support of https to curl.
  exit 1
}

ISTIO_WASM_STATS_PLUGIN_URL=${ISTIO_WASM_STATS_PLUGIN_URL:-https://storage.googleapis.com/istio-artifacts/wasm/stats_1_3_1.wasm}
ISTIO_WASM_METADATAEXCHANGE_PLUGIN_URL=${ISTIO_WASM_METADATAEXCHANGE_PLUGIN_URL:-https://storage.googleapis.com/istio-artifacts/wasm/metadata_exchange_1_3_1.wasm}

# Set the value of DOWNLOAD_COMMAND (either curl or wget)
set_download_command

mkdir -p ${OUTDIR}/wasm

# Download stats plugin
${DOWNLOAD_COMMAND} "${ISTIO_WASM_STATS_PLUGIN_URL}" -o ${OUTDIR}/wasm/stats_1_3_1.wasm

# Download metadata exchange plugin.
${DOWNLOAD_COMMAND} "${ISTIO_WASM_METADATAEXCHANGE_PLUGIN_URL}" -o ${OUTDIR}/wasm/metadata_exchange_1_3_1.wasm
