#!/bin/bash
#
# Copyright 2019 Istio Authors. All Rights Reserved.
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

set -e

# IF ECHO_ARGS is unset, make it an empty string.
ECHO_ARGS=${ECHO_ARGS:-}
# Split ECHO_ARGS by spaces.
IFS=' ' read -r -a ECHO_ARGS_ARRAY <<< "$ECHO_ARGS"

ISTIO_LOG_DIR=${ISTIO_LOG_DIR:-/var/log/istio}

# Run the pilot agent and Envoy
/usr/local/bin/istio-start.sh&

# Start the echo server.
"/usr/local/bin/server" "${ECHO_ARGS_ARRAY[@]}"
