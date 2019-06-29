#!/bin/bash

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

# Applies yamllint to validate yaml files to the source tree

set -e

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )

ROOTDIR=$SCRIPTPATH/..
cd "$ROOTDIR"

PKGS=${PKGS:-"."}
if [[ -z ${YAML_FILES} ]];then
    YAML_FILES=$(find "${PKGS}" -type f -name '*.yaml' | grep -v ./vendor | grep -v -E "\/templates\/" | grep -v -E "template")
fi
 
for YAML_FILE in ${YAML_FILES}; do
        echo "${YAML_FILE}"
        yamllint -c ./bin/yamllintrules "${YAML_FILE}"
done

exit $?