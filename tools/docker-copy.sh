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

INPUTS=("${@}")
TARGET_ARCH=${TARGET_ARCH:-amd64}
DOCKER_WORKING_DIR=${INPUTS[${#INPUTS[@]}-1]}
FILES=("${INPUTS[@]:0:${#INPUTS[@]}-1}")

set -eu;

# detect_arch returns "amd64", "arm64", or "" depending on if the file is arch specific
function detect_arch() {
  FILE=${1}
  extension="${FILE##*.}"
  if [[ "${extension}" == "deb" ]]; then
    arch="$(dpkg-deb --info "${FILE}" | grep Arch | cut -d: -f 2 | cut -c 2-)"
    echo "${arch}"
    return 0
  elif [[ "${extension}" == "rpm" ]]; then
    arch="$(rpm -qp --queryformat '%{arch}' "${FILE}" 2>/dev/null || true)"
    case ${arch} in
      "x86_64")
        echo "amd64"
        return 0
        ;;
      "aarch64")
        echo "arm64"
        return 0
        ;;
    esac
  fi
  FILE_INFO=$(file "${FILE}" || true)
  # Here we need to support ELF (unix-based) and PE (Windows)
  # executable formats.
  if [[ ${FILE_INFO} == *"executable"* ]]; then
    case ${FILE_INFO} in
      *x86-64*)
        echo "amd64"
        return 0
        ;;
      *aarch64*)
        echo "arm64"
        return 0
        ;;
    esac
  fi
}

function may_copy_into_arch_named_sub_dir() {
  local FILE=${1}
  local COPY_ARCH_RELATED=${COPY_ARCH_RELATED:-1}

  local arch
  arch="$(detect_arch "${FILE}")"
  local FILE_INFO
  FILE_INFO=$(file "${FILE}" || true)

  if [[ "${arch}" != "" && ${COPY_ARCH_RELATED} == 1 ]]; then
    # if other arch files exists, should copy too.
    for ARCH in "amd64" "arm64"; do
      # like file `out/linux_amd64/pilot-discovery`
      # should check  `out/linux_arm64/pilot-discovery` exists then do copy

      local FILE_ARCH_RELATED=${FILE/linux_${TARGET_ARCH}/linux_${ARCH}}

      if [[ ${FILE_ARCH_RELATED} != "${FILE}" && -f ${FILE_ARCH_RELATED} ]]; then
        COPY_ARCH_RELATED=0 may_copy_into_arch_named_sub_dir "${FILE_ARCH_RELATED}"
      fi
    done
  fi

  # For arch specific, will put an arch named sub dir like
  #   arm64/
  #   amd64/
  dst="${DOCKER_WORKING_DIR}"
  if [[ "${arch}" != "" ]]; then
    dst+="/${arch}/"
  fi
  mkdir -p "${dst}"
  cp -rp "${FILE}" "${dst}"

  # Based on type, explicit set permissions. These may differ on host machine due to umask, so always override.
  out="${dst}/$(basename "${FILE}")"
  if [[ -d "${out}" ]]; then
    chmod -R a+r "${out}"
  elif [[ -x "${out}" ]]; then
    chmod 755 "${out}"
  else
    chmod a+r "${out}"
  fi
}

mkdir -p "${DOCKER_WORKING_DIR}"
for FILE in "${FILES[@]}"; do
  may_copy_into_arch_named_sub_dir "${FILE}"
done
