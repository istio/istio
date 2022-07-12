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

# shellcheck disable=all

./local-test-utils/reset-kind.sh

./local-test-utils/kind-registry.sh

export GOPRIVATE=github.com/solo-io/istio-api-sidecarless

HUB=${HUB:-"localhost:5000"}
TAG=${TAG:-"ambient"}
PROFILE=${PROFILE:-"ambient"}


# if localhost is in hub, use crane builder
if [[ $HUB == *localhost* ]]; then
    BUILDER="--builder=crane"
fi

tools/docker --targets=pilot,proxyv2,app --hub=$HUB --tag=$TAG --push $BUILDER

tools/docker --targets=pilot,proxyv2,app --hub=$HUB --tag=$TAG --push # consider --builder=crane

# Install Istio without gateway or webhook
# profile can be "ambient" or "ambient-gke" or "ambient-aws"
# Mesh config options are optional to improve debugging
CGO_ENABLED=0 go run istioctl/cmd/istioctl/main.go install -d manifests/ --set hub=$HUB --set tag=$TAG -y \
  --set profile=$PROFILE --set meshConfig.accessLogFile=/dev/stdout --set meshConfig.defaultHttpRetryPolicy.attempts=0

if [ -z "$BUILDER" ]; then
./local-test-utils/refresh-istio-images.sh
fi

kubectl apply -f local-test-utils/samples/

# Turn mesh on
./redirect.sh ambient

sleep 5

# Update pod membership (will move to CNI). can stop it after it does 1 iteration if pods don't change
./tmp-update-pod-set.sh
