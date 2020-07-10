#!/usr/bin/env bash

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

if [[ $# -le 0 ]]; then
    echo Require more than one argument to protoc.
    exit 1
fi

RETRY_COUNT=3

api=$(go list -mod=readonly -m -f "{{.Dir}}" istio.io/api)

# This occasionally flakes out, so have a simple retry loop
for (( i=1; i <= RETRY_COUNT; i++ )); do
  protoc -I"${REPO_ROOT}"/common-protos -I"${api}" "$@" && break

  ret=$?
  echo "Attempt ${i}/${RETRY_COUNT} to run protoc failed with exit code ${ret}" >&2
  (( i == RETRY_COUNT )) && exit $ret
done
