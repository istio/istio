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
# Ran from the release/Dockerfile[.in] to invoke fpm with common arguments
# Assumes the layout from the Dockerfiles (/stage source, /tgz destination etc)
fpm -v $(/stage/usr/bin/fortio version -s) -n fortio --license "Apache License, Version 2.0" \
    --category utils --url https://fortio.org/ --maintainer fortio@fortio.org \
    --description "Fortio is a load testing library, command line tool, advanced echo server and web UI in go (golang)." \
    -s tar -t $1 /tgz/*.tgz
