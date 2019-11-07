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

# Set up env ISTIO if not done yet
if [[ -z "${ISTIO// }" ]]; then
    export ISTIO=$GOPATH/src/istio.io
    echo 'Set ISTIO to' "$ISTIO"
fi

case "$OSTYPE" in
  darwin*)  sh  setup_kubectl_config_host.sh
	;;
  linux*)   sh  setup_dockerdaemon_linux.sh
	     sh  setup_kubectl_config_host.sh
	;;
  *)        echo "unsupported: $OSTYPE"
	;;
esac
