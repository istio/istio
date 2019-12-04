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

package v1alpha1

import (
	authn "istio.io/api/authentication/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
)

// GetMutualTLSMode returns the mTLS mode for given. If the policy is nil, or doesn't define mTLS, it returns MTLSDisable.
func GetMutualTLSMode(policy *authn.Policy) model.MutualTLSMode {
	if mTLSSetting := GetMutualTLS(policy); mTLSSetting != nil {
		if mTLSSetting.GetMode() == authn.MutualTls_STRICT {
			return model.MTLSStrict
		}
		return model.MTLSPermissive
	}
	return model.MTLSDisable
}
