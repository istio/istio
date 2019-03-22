#!/bin/ash

# Copyright 2019 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

set -eu

locks=$(find ./releaselocks -type d -name 'release-*' | sort)
fail=none

for lock in $locks; do
    echo "Testing $lock"
    protolock status --lockdir=${lock} | sort -fd > status && :
    diff status ${lock}/proto.lock.status > diff.out || fail=$lock
    rm status
    if [[ $fail != "none" ]]; then
        echo "Error $fail"
        cat diff.out
        rm diff.out
        exit 1
    fi
    rm diff.out
done
