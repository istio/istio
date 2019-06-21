#!/usr/bin/env bash

# Copyright 2019 The Istio Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

ISTIO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

# create a nice clean place to put our new licenses
# must be in the user dir (e.g. ISTIO_ROOT) in order for the docker volume mount
# to work with docker-machine on macs
mkdir -p "${ISTIO_ROOT}/_tmp"
_tmpdir="$(mktemp -d "${ISTIO_ROOT}/_tmp/istio-vendor.XXXXXX")"

if [[ -z ${KEEP_TMP:-} ]]; then
    KEEP_TMP=false
fi

function cleanup {
  # make go module dirs writeable
  chmod -R +w "${_tmpdir}"
  if [ "${KEEP_TMP}" == "true" ]; then
    echo "Leaving ${_tmpdir} for you to examine or copy. Please delete it manually when finished. (rm -rf ${_tmpdir})"
  else
    echo "Removing ${_tmpdir}"
    rm -rf "${_tmpdir}"
  fi
}
cleanup EXIT

# Copy the contents of the istio directory into the nice clean place (which is NOT shaped like a GOPATH)
_istiotmp="${_tmpdir}"
mkdir -p "${_istiotmp}"
# should create ${_istioctmp}/istio
git archive --format=tar --prefix=istio/ "$(git write-tree)" | (cd "${_istiotmp}" && tar xf -)
_istiotmp="${_istiotmp}/istio"

# Do all our work in module mode
export GO111MODULE=on

pushd "${_istiotmp}" > /dev/null 2>&1
  # Destroy deps in the copy of the istio tree
  rm -rf ./vendor

  # Recreate the vendor tree using the nice clean set we just downloaded
  bin/update-vendor.sh
popd > /dev/null 2>&1

ret=0

pushd "${ISTIO_ROOT}" > /dev/null 2>&1
  # Test for diffs
  if ! _out="$(diff -Naupr --ignore-matching-lines='^\s*\"GoVersion\":' go.mod "${_istiotmp}/go.mod")"; then
    echo "Your go.mod file is different:" >&2
    echo "${_out}" >&2
    echo "Vendor Verify failed." >&2
    echo "If you're seeing this locally, run the below command to fix your go.mod:" >&2
    echo "bin/update-vendor.sh" >&2
    ret=1
  fi

  if ! _out="$(diff -Naupr -x "BUILD" -x "AUTHORS*" -x "CONTRIBUTORS*" vendor "${_istiotmp}/vendor")"; then
    echo "Your vendored results are different:" >&2
    echo "${_out}" >&2
    echo "Vendor Verify failed." >&2
    echo "${_out}" > vendordiff.patch
    echo "If you're seeing this locally, run the below command to fix your directories:" >&2
    echo "bin/update-vendor.sh" >&2
    ret=1
  fi
popd > /dev/null 2>&1

if [[ ${ret} -gt 0 ]]; then
  exit ${ret}
fi

echo "Vendor Verified."
# ex: ts=2 sw=2 et filetype=sh
