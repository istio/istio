#!/bin/bash

# WARNING: DO NOT EDIT, THIS FILE IS PROBABLY A COPY
#
# The original version of this file is located in the https://github.com/istio/common-files repo.
# If you're looking at this file in a different repo and want to make a change, please go to the
# common-files repo, make the change there and check it in. Then come back to this repo and run
# "make updatecommon".

# Copyright 2018 Istio Authors
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

# Applies requisite code formatters to the source tree

set -e

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR=$(dirname "${SCRIPTPATH}")
cd "${ROOTDIR}"

# Go format tool to use
# While 'goimports' is preferred we temporarily use 'gofmt' until https://github.com/golang/go/issues/28200 is resolved
GO_FMT_TOOL=goimportsdocker

PKGS=${PKGS:-"."}
if [[ -z ${GO_FILES} ]];then
  GO_FILES=$(find "${PKGS}" -type f -name '*.go' ! -name '*.gen.go' ! -name '*.pb.go' ! -name '*mock*.go' | grep -v ./vendor)
fi

# need to pin goimports to align with golangci-lint. SHA is from x/tools repo
if [ $GO_FMT_TOOL = "goimportsdocker" ]; then
  GO_IMPORTS_DOCKER="gcr.io/istio-testing/goimports:379209517ffe"
  tool="docker run -i --rm -v ${ROOTDIR}:${ROOTDIR} -w ${ROOTDIR} ${GO_IMPORTS_DOCKER} /goimports"
  fmt_args="-w -local istio.io"
fi

if [ $GO_FMT_TOOL = "gofmt" ]; then
  tool=gofmt
  fmt_args="-w"
fi

echo "Formatting the source files"
# shellcheck disable=SC2086
$tool ${fmt_args} ${GO_FILES}
exit $?
