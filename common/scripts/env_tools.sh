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

# TODO: Use the below in every script that uses the -i option on sed
find_inplace_sed() {
    local tmpfile=`mktemp`
    local POSSIBLE_INPLACE_SED_CMDS=("sed -i" "sed -i ''")
    local sed_cmd
    unset INPLACE_SED
    for sed_cmd in "${POSSIBLE_INPLACE_SED_CMDS[@]}"; do
        echo orig > ${tmpfile}
        if eval ${sed_cmd} 's/orig/new/g' ${tmpfile} >/dev/null 2>&1 ; then
            INPLACE_SED="${sed_cmd}"
            break
        fi
    done
    rm $tmpfile
    if [ -z ${INPLACE_SED+x} ]; then
        echo "Cannot find a valid inplace sed command from ${POSSIBLE_INPLACE_SED_CMDS}"
        return 1
    fi
    return 0
}
