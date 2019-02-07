#!/bin/bash

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

SCRIPTPATH=$( cd "$(dirname "$0")" && pwd -P )
ROOTDIR=$SCRIPTPATH/..
pushd "$ROOTDIR" || exit

if ! git diff --quiet install/kubernetes/helm/subcharts/grafana/dashboards/; then
    echo "Grafana dashboards have unstaged changes, please stage before linting."
    popd || exit
    exit 1
fi

install/kubernetes/helm/subcharts/grafana/fix_datasources.sh

if ! git diff --quiet install/kubernetes/helm/subcharts/grafana/dashboards/; then
    echo "Grafana dashboards' datasources fixed, please add to commit."
    popd || exit
    exit 1
fi

popd || exit
