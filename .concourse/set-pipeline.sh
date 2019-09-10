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

cd  "$( dirname "${BASH_SOURCE[0]}" )"

if [ -z $PIPELINE ] ; then
    PIPELINE=istio-installer
fi
if [ -z $TARGET ] ; then
    TARGET=concourse-sapcloud
fi

if [ -n "$1" ] ; then
    fly --target $TARGET login --concourse-url $1
fi

fly -t $TARGET set-pipeline -c concourse.yaml -p $PIPELINE