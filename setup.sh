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

tools/docker --targets=pilot,proxyv2,app,install-cni --hub=$HUB --tag=$TAG --push $BUILDER

kubectl label namespace default istio.io/dataplane-mode=ambient --overwrite

# Install Istio without gateway or webhook
# profile should be "ambient"
# Mesh config options are optional to improve debugging
cat <<EOF | CGO_ENABLED=0 go run istioctl/cmd/istioctl/main.go install -d manifests/ -y -f -
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
metadata:
  name: ingress-gateway
  namespace: istio-system
spec:
  hub: ${HUB}
  tag: ${TAG}
  profile: ambient
  meshConfig:
    accessLogFile: /dev/stdout
    defaultHttpRetryPolicy:
      attempts: 0
    defaultConfig:
      proxyMetadata:
        ISTIO_META_DNS_CAPTURE: "true"
        ISTIO_META_DNS_AUTO_ALLOCATE: "true"
        DNS_PROXY_ADDR: "0.0.0.0:15053"
EOF

if [ -z "$BUILDER" ]; then
./local-test-utils/refresh-istio-images.sh
fi

kubectl apply -f local-test-utils/samples/
