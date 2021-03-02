// Copyright Istio Authors
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

package features

import "istio.io/pkg/env"

// Only read env variables in tests so that they are not set accidentally in production code.
var _ = registerTestOnlyFeatures

func registerTestOnlyFeatures() {
	EnableAdminEndpoints = env.RegisterBoolVar("ENABLE_ADMIN_ENDPOINTS", false,
		"If this is set to true, dangerous admin endpoins will be exposed on the debug interface.").Get()
}
