// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package factory

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authn"
	"istio.io/istio/pilot/pkg/security/authn/v1alpha1"
)

// NewPolicyApplier returns the appropriate (policy) applier, depends on the versions of the policy exists
// for the given service instance.
func NewPolicyApplier(push *model.PushContext,
	serviceInstance *model.ServiceInstance) authn.PolicyApplier {
	// TODO: check v1alpha2 policy and returns alpha2 applier, if exists.
	service := serviceInstance.Service
	// TODO GregHanson add support for authn policy label matching
	port := serviceInstance.Endpoint.ServicePort
	authnPolicy, _ := push.AuthenticationPolicyForWorkload(service, port)
	return v1alpha1.NewPolicyApplier(authnPolicy)
}
