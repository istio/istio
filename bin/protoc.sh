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

if [[ $# -le 0 ]]; then
    echo Require more than one argument to protoc.
    exit 1
fi

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR="$(dirname "$SCRIPTPATH")"

# Ensure expected GOPATH setup
if [ "$ROOTDIR" != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
  die "Istio not found in GOPATH/src/istio.io/"
fi

api=$(go list -m -f "{{.Dir}}" istio.io/api)
protobuf=$(go list -m -f "{{.Dir}}" github.com/gogo/protobuf)
gogo_genproto=$(go list -m -f "{{.Dir}}" istio.io/gogo-genproto)

gen_img=gcr.io/istio-testing/build-tools:2019-08-16

docker run \
  -i \
  --rm \
  -v "$ROOTDIR:$ROOTDIR" \
  -v "${api}:/protos/istio.io/api" \
  -v "${protobuf}:/protos/github.com/gogo/protobuf" \
  -v "${gogo_genproto}:/protos/istio.io/gogo-genproto" \
  -w "$(pwd)" \
  --entrypoint /usr/bin/protoc \
  $gen_img \
  -I/protos/istio.io/api \
  -I/protos/github.com/gogo/protobuf \
  -I/protos/istio.io/gogo-genproto/googleapis \
  "$@"
