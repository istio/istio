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

WD=$(dirname "$0")
WD=$(cd "$WD" || exit; pwd)

set -eux

# shellcheck source=prow/asm/tester/scripts/libs/asm-lib.sh
source "${WD}/libs/asm-lib.sh"

# dispatch makes it possible to call individual functions from our large
# bash libraries from our Go scripts.
# Parameters: $1: name of the function to execute
#             $2: arguments to pass to the function
function dispatch() {
	cmd="$1"; shift 1;
	eval "$cmd" "$@"
}

dispatch "$@"
