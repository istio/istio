#! /bin/bash
# Copyright 2017 Istio Authors.
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
# To be run by ../Makefile as release/release.sh
set -x
set -e
# Release tgz Dockerfile is based on the normal docker one
cat Dockerfile release/Dockerfile.in > release/Dockerfile
docker build -f release/Dockerfile -t fortio/fortio:release .
DOCKERID=$(docker create --name fortio_release fortio/fortio:release x)
function cleanup {
  docker rm fortio_release
}
trap cleanup EXIT
set -o pipefail
# docker cp will create 2 level of dir if first one exists, make sure it doesnt
rm -f release/tgz/*.tgz
rmdir release/tgz || true
docker cp -a fortio_release:/tgz/ release/tgz
tar tvfz release/tgz/*.tgz
# then save the result 1 level up
mv release/tgz/*.tgz release/
rmdir release/tgz
