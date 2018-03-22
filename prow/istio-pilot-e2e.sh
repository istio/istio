#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

source "${ROOT}/prow/istio-pilot-e2e-common.sh"

# Run tests with auth disabled
make depend e2e_pilot HUB="${HUB}" TAG="${GIT_SHA}" TESTOPTS="-mixer=true -use-sidecar-injector=true -use-admission-webhook=false -auth_enable=false -v1alpha3=false"

# Run tests with auth enabled
make depend e2e_pilot HUB="${HUB}" TAG="${GIT_SHA}" TESTOPTS="-mixer=true -use-sidecar-injector=true -use-admission-webhook=false -auth_enable=true -v1alpha3=false"
