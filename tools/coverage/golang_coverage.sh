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

# This script collects unit test coverage metrics and upload the data to GCS.
# See b/179833499

set -o errexit
set -o nounset
set -o pipefail
set -x

PKG_ROOT=$1
testdirs() {
  find -L "${PKG_ROOT}" \
    -not \( -path "${PKG_ROOT}/tests/integration/*" -prune \) \
    -not \( -path "${PKG_ROOT}/samples/*" -prune \) \
    -not \( -path "${PKG_ROOT}/prow/asm/tester/*" -prune \) \
    -not \( -path "${PKG_ROOT}/pkg/test/framework/components/echo/kube/*" -prune \) \
    -name \*_test.go -print0 | xargs -0n1 dirname | \
    sed "s|^${PKG_ROOT}/|./|" | LC_ALL=C sort -u
}

coverprofile="${PKG_ROOT}"/coverage.out
if [[ -z $coverprofile ]]; then
  echo "usage: ./<SCRIPT> <coverprofile>"
  exit 1
fi
if [[ x"${coverprofile:(-4)}" != x.out ]] ; then
  echo "coverprofile ${coverprofile} must end in .out"
  exit 1
fi
# Remove any old cover profile so that the run is clean.
rm -f "${coverprofile}"
echo "Verifying coverage"
# TODO: use a make command instead.
# Unquote $(testdirs) to allow word splitting
# shellcheck disable=SC2046
go test -coverprofile="${coverprofile}" $(testdirs)
