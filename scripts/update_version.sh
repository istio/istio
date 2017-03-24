#!/bin/bash

# Copyright 2017 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="${ROOT}/istio.VERSION"

REGISTRY='docker.io/istio'
INIT_KEY='INIT'
MIXER_KEY='MIXER'
RUNTIME_KEY='RUNTIME'

function usage() {
  [[ -n "${1}" ]] && echo "${1}"

  cat <<EOF
usage: ${BASH_SOURCE[0]} [options ...]"
  options:
    -i ... init docker image version
    -m ... mixer docker image version
    -r ... runtime docker image version
    -R ... registry to use. Default is ${REGISTRY}
EOF
  exit 2
}

while getopts :i:m:r:R: arg; do
  case ${arg} in
    i) INIT_VERSION="${OPTARG}";;
    m) MIXER_VERSION="${OPTARG}";;
    r) RUNTIME_VERSION="${OPTARG}";;
    R) REGISTRY="${OPTARG}";;
    *) usage;;
  esac
done

function error_exit() {
  # ${BASH_SOURCE[1]} is the file name of the caller.
  echo "${BASH_SOURCE[1]}: line ${BASH_LINENO[0]}: ${1:-Unknown Error.} (exit ${2:-1})" 1>&2
  exit ${2:-1}
}

function update_files() {
  local key="${1}"
  local new_version="${2}"
  # Lower case
  local image="${key,,}"
  local old_value="$(read_version ${key})"
  [[ -z ${old_value} ]] && error_exit "Could not find current version for ${key}"
  local new_value="${REGISTRY}/${image}:${new_version}"

  local files=($(find ./ -type f -and -not -path "./.git*" \
    -exec grep -l -e "${old_value}" {} \;))

  for f in ${files[@]}; do
    echo "Updating ${f}"
    sed -i "s,${old_value},${new_value},g" "${f}" \
      || error_exit "Could not update ${f} from ${old_value} to ${new_value}"
  done
}

function read_version() {
  local key="${1}"
  local version="$(grep -oP -e "${key}\s=\s\"\K.*(?=\")" ${VERSION_FILE})"
  echo "${version}"
}

[[ -n "${INIT_VERSION}" ]] && update_files "${INIT_KEY}" "${INIT_VERSION}"
[[ -n "${MIXER_VERSION}" ]] && update_files "${MIXER_KEY}" "${MIXER_VERSION}"
[[ -n "${RUNTIME_VERSION}" ]] && update_files "${RUNTIME_KEY}" "${RUNTIME_VERSION}"
