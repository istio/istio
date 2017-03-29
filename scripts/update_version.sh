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
QUAL_VERSION_FILE="${ROOT}/istio.VERSION.qual"

REGISTRY='docker.io/istio'
INIT_KEY='INIT'
MIXER_KEY='MIXER'
RUNTIME_KEY='RUNTIME'
QUAL=false

function usage() {
  [[ -n "${1}" ]] && echo "${1}"

  cat <<EOF
usage: ${BASH_SOURCE[0]} [options ...]"
  options:
    -i ... init docker image version
    -m ... mixer docker image version
    -r ... runtime docker image version
    -Q ... Use "${QUAL_VERSION_FILE}" as input
    -R ... registry to use. Default is ${REGISTRY}
EOF
  exit 2
}

while getopts :i:m:r:QR: arg; do
  case ${arg} in
    i) INIT_VERSION="${OPTARG}";;
    m) MIXER_VERSION="${OPTARG}";;
    r) RUNTIME_VERSION="${OPTARG}";;
    Q) QUAL=true;;
    R) REGISTRY="${OPTARG}";;
    *) usage;;
  esac
done

function error_exit() {
  # ${BASH_SOURCE[1]} is the file name of the caller.
  echo "${BASH_SOURCE[1]}: line ${BASH_LINENO[0]}: ${1:-Unknown Error.} (exit ${2:-1})" 1>&2
  exit ${2:-1}
}

function set_git() {
  if [[ ! -e "${HOME}/.gitconfig" ]]; then
    cat > "${HOME}/.gitconfig" << EOF
[user]
  name = istio-testing
  email = istio.testing@gmail.com
EOF
  fi
}

function update_files() {
  local old_value="${1}"
  local new_value="${2}"

  local files=($(find ./ -type f -and -not -path "./.git*" \
    -exec grep -l -e "${old_value}" {} \;))

  for f in ${files[@]}; do
    echo "Updating ${f}"
    sed -i "s,${old_value},${new_value},g" "${f}" \
      || error_exit "Could not update ${f} from ${old_value} to ${new_value}"
  done
}

function update_versions() {
  local key="${1}"
  local new_version="${2}"
  # Lower case
  local image="${key,,}"
  local old_value="$(read_version ${key} ${VERSION_FILE})"
  [[ -z ${old_value} ]] && error_exit "Could not find current version for ${key}"
  local new_value="${REGISTRY}/${image}:${new_version}"

  update_files "${old_value}" "${new_value}"
}

function update_qual_versions() {
  local key="${1}"
  local old_value="$(read_version ${key} ${VERSION_FILE})"
  local new_value="$(read_version ${key} ${QUAL_VERSION_FILE})"

  if [[ "${old_value}" != "${new_value}" ]]; then
    update_files "${old_value}" "${new_value}"
  fi
}

function read_version() {
  local key="${1}"
  local file="${2}"
  local version="$(grep -oP -e "${key}\s=\s\"\K.*(?=\")" ${file})"
  echo "${version}"
}

function create_commit() {
  set_git
  # If nothing to commit skip
  check_git_status && return

  echo 'Creating a commit'
  git commit -a -m "Updating istio version" \
    || error_exit 'Could not create a commit'

}

function check_git_status() {
  local git_files="$(git status -s)"
  [[ -z "${git_files}" ]] && return 0
  return 1
}

function update_qual_version_file() {
  echo "# DO NOT EDIT. AUTO-GENERATED FILE." > "${QUAL_VERSION_FILE}"
  grep -v '#' "${VERSION_FILE}" >> "${QUAL_VERSION_FILE}"
}

check_git_status \
  || error_exit "You have modified files. Please commit or reset your workspace."

if [[ ${QUAL} == true ]]; then
  update_qual_versions "${INIT_KEY}"
  update_qual_versions "${MIXER_KEY}"
  update_qual_versions "${RUNTIME_KEY}"
else
  [[ -n "${INIT_VERSION}" ]] && update_versions "${INIT_KEY}" "${INIT_VERSION}"
  [[ -n "${MIXER_VERSION}" ]] && update_versions "${MIXER_KEY}" "${MIXER_VERSION}"
  [[ -n "${RUNTIME_VERSION}" ]] && update_versions "${RUNTIME_KEY}" "${RUNTIME_VERSION}"
fi

update_qual_version_file
create_commit
