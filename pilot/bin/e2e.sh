#!/bin/bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
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
################################################################################
#
# Run integration tests
#
# It is a simple shim over test/integration/driver.go that accepts the same set of flags.
# Please add new flags to the Go test driver directly instead of extending this file.
# The additional steps that the script performs are:
# - set default docker tag based on a timestamp and user name
# - build and push docker images, including this repo pieces and proxy.

set -o errexit
set -o nounset
set -o pipefail
set -x

args=""
hub="gcr.io/istio-testing"
tag=$(whoami)_$(date +%y%m%d_%H%M%S)

while [[ $# -gt 0 ]]; do
    case "$1" in
        -tag) tag="$2"; shift ;;
        -hub) hub="$2"; shift ;;
        *) args=$args" $1" ;;
    esac
    shift
done

bin/push-docker -hub $hub -tag $tag
bazel run //pilot/test/integration -- --logtostderr $args -hub $hub -tag $tag
