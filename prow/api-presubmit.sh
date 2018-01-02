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


#######################################
# Presubmit script triggered by Prow. #
#######################################

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

die () {
    echo "$@"
    exit 1
}

mv ${GOPATH}/src/github.com/istio ${GOPATH}/src/istio.io/
cd ${GOPATH}/src/istio.io/api

WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

cd ${ROOT}

./scripts/generate-protos.sh || die "Could not generate *.pb.go"

if [[ -n $(git status --porcelain) ]]; then
    git status
    git diff
    die "Repo has unstaged changes. Re-run ./scripts/generate-protos.sh"
fi

exit 0
