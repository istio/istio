#!/usr/bin/env bash

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

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR=$(dirname "$SCRIPTPATH")

# Ensure expected GOPATH setup
if [ "$ROOTDIR" != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
  die "Istio not found in GOPATH/src/istio.io/"
fi

gen_img=gcr.io/istio-testing/go_generate_dependency:2018-07-26

docker run  -i --volume /var/run/docker.sock:/var/run/docker.sock \
  -e "GOPATH=/go:$GOPATH" --rm --entrypoint counterfeiter -v "$ROOTDIR:$ROOTDIR" -w "$(pwd)" $gen_img "$@"
