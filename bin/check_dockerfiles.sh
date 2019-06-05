#!/bin/sh

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

# Find Dockerfiles running an apt-get update but no uprade

BASE_DIR="$(cd "$(dirname "${0}")" && pwd -P)"
ISTIO_ROOT="$(cd "$(dirname "${BASE_DIR}")" && pwd -P)"

STALE_DOCKERFILES=$( \
  find "${ISTIO_ROOT}" \
  -name 'Dockerfile*' | \
  while read -r f; do
    if [ "" != "$(grep "apt-get update" $f)" ] ; then \
      if [ "" == "$(grep "apt-get upgrade" $f)" ] ; then \
        echo "$f:"; \
      fi ; \
    fi; \
  done)

if [ "" != $STALE_DOCKERFILES ]; then
  exit 1
fi
