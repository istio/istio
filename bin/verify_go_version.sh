#!/bin/bash
#
# Copyright 2018 Istio Authors. All Rights Reserved.
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
#

# The script verifies that go version is as required

set -o errexit
set -o nounset
set -o pipefail

REQUIRED_GO_VERSION_MAJOR=1
REQUIRED_GO_VERSION_MINOR=9

GO_VERSION=$(go version | sed 's/go version go\([[:digit:]]\.[[:digit:]]\.[[:digit:]]\).*/\1/g')
GO_VERSION_MAJOR=$(echo $GO_VERSION | cut -f1 -d.)
GO_VERSION_MINOR=$(echo $GO_VERSION | cut -f2 -d.)

function print_error_and_exit {
    echo Your Go version is ${GO_VERSION}. Istio compiles with Go 1.9+. Please update your Go.
    exit 1
}

if [ "$GO_VERSION_MAJOR" -lt "${REQUIRED_GO_VERSION_MAJOR}" ]; then
   print_error_and_exit
fi

if [ "$GO_VERSION_MAJOR" -le "${REQUIRED_GO_VERSION_MAJOR}" -a \
     "$GO_VERSION_MINOR" -lt "${REQUIRED_GO_VERSION_MINOR}" ]; then
  print_error_and_exit
fi
