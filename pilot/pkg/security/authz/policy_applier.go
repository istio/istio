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

package authz

import (
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
)

// PolicyApplier is the interface provides essential functionalities to help config Envoy (xDS) to enforce
// authorization policy.
type PolicyApplier interface {
	// BuildHTTP returns the HTTP filters built from the authorization policy.
	BuildHTTP() []*hcm.HttpFilter

	// BuildTCP returns the TCP filters built from the authorization policy.
	BuildTCP() []*listener.Filter
}

// Option general setting to control behavior
type Option struct {
	IsCustomBuilder  bool
	UseAuthenticated bool
}
