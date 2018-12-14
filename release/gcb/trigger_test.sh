#!/bin/bash

# Copyright 2018 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

set -x

GOPATH=$PWD/go
mkdir -p go/bin
GOBIN=$GOPATH/bin

time go get -u istio.io/test-infra/toolbox/githubctl

# this setting is required by githubctl, which runs git commands
git config --global user.name "TestRunnerBot"
git config --global user.email "testrunner@istio.io"

"$GOBIN/githubctl" \
    --token_file="$GITHUB_TOKEN_FILE" \
    --op=relPipelineQual \
    --base_branch="$CB_BRANCH" \
    --tag="$CB_VERSION" \
    --pipeline="$CB_PIPELINE_TYPE"
