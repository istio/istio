#!/bin/bash
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
################################################################################

set -o errexit
set -o nounset
set -o pipefail
set -x

source "/workspace/gcb_env.sh"
# This script pushes to build docker hub with the lastest tag

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
"${SCRIPTPATH}/rel_push_docker.sh" -h "${CB_PUSH_DOCKER_HUBS}" -t "${CB_BRANCH}-latest-daily"
