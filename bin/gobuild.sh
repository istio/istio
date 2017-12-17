#!/bin/bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
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
# This script builds and link stamps the output
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT=$1

set -x

GOOPRN=${GOOPRN:-build}
GOINCREMENTAL=-i

if [[ ${OUT} == "__run__" ]];then
    OUT=""
    GOINCREMENTAL=""
    GOOPRN=run
else
    OUT="-o ${OUT}"
fi

shift
VERSION_MODULE=$1 # istio.io/istio/mixer/pkg/version
shift

set -e

VERBOSE=${VERBOSE:-"0"}
V=""
if [[ "${VERBOSE}" == "1" ]];then
    V="-x"
fi

GOOS=${GOOS:-linux}
GOARCH=${GOARCH:-amd64}
GOBIN=${GOBIN:-go}

GOOS=${GOOS} GOARCH=${GOARCH} CGO_ENABLED=0 ${GOBIN} ${GOOPRN} ${V} ${GOINCREMENTAL} ${OUT} \
	-ldflags "-extldflags -static $(${ROOT}/bin/get_workspace_status ${VERSION_MODULE})" "$*"
