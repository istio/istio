#!/bin/bash
#
# Copyright 2017 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

SCRIPTDIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )

istioctl delete route-rule productpage-default
istioctl delete route-rule reviews-default
istioctl delete route-rule ratings-default
istioctl delete route-rule details-default
istioctl delete route-rule reviews-test-v2
istioctl delete route-rule ratings-test-delay
#istioctl delete mixer-rule ratings-ratelimit

kubectl delete -f $SCRIPTDIR/bookinfo-istio.yaml
kubectl delete -f $SCRIPTDIR/../../ingress-controller.yaml
kubectl delete -f $SCRIPTDIR/../../../kubernetes/istio-install
