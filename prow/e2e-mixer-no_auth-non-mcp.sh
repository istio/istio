#!/bin/bash

# Copyright 2017 Istio Authors
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

#######################################
#                                     #
#        mixer-e2e (v1alpha3)         #
#             MCP variant             #
#                                     #
#######################################

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

# Run tests with auth disabled
#echo 'Running mixer e2e tests (v1alpha3, noauth)'
./prow/e2e-suite.sh --use_mcp=false --single_test e2e_mixer
