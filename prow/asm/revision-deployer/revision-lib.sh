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

# Install ASM revisions from the specified revision configuration file.
# Parameters: $1 - REVISION_CONFIG: Path to the revision config file.
#             $2 - PKG: Path of kpt package.
#             $3 - WIP: GKE or HUB
#             $4 - CONTEXTS: array of k8s contexts
function install_asm_revisions() {
  local REVISION_CONFIG_PATH="$1"; shift
  local PKG="$1"; shift
  local WIP="$1"; shift
  local CONTEXTS=("${@}")

  # REVISION_CONFIG_PATH is relative to ./revision-deployer/configs
  REVISION_CONFIG_PATH="${WD}/revision-deployer/configs/${REVISION_CONFIG_PATH}"

  # parse configurations for each revision from the config file
  # some of these configs will get turned into additional scriptaro flags
  echo "Parsing revision config from ${REVISION_CONFIG_PATH}..."
  for k in $(jq '.revisions | keys | .[]' "${REVISION_CONFIG_PATH}"); do
    v=$(jq -r ".revisions[$k]" "${REVISION_CONFIG_PATH}");
    REVISION=$(jq -r '.name // ""' <<< "$v");
    CA=$(jq -r '.ca // ""' <<< "$v");
    OVERLAY=$(jq -r '.overlay // ""' <<< "$v");
    SCRIPTARO_FLAGS=$(jq -r '.scriptaro_flags // ""' <<< "$v")

    if [ -n "$REVISION" ]; then
      SCRIPTARO_FLAGS+=" --revision_name ${REVISION}"
    fi

    echo "Installing ASM CP revision ${REVISION}..."
    echo "--> ca: $CA, overlay: $OVERLAY, scriptaro flags: $SCRIPTARO_FLAGS"

    # Script is sourced from `integ-suite-kind.sh` so we can count on `asm-lib.sh` being available
    install_asm "${PKG}" "${CA}" "${WIP}" "${OVERLAY}" "${SCRIPTARO_FLAGS}" "${CONTEXTS[@]}"
  done
}