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

package jwt

const (
	PolicyThirdParty = "third-party-jwt"
	PolicyFirstParty = "first-party-jwt"
)

type JwksFetchMode int

const (
	// Istiod is used to indicate Istiod ALWAYS fetches the JWKs server
	Istiod JwksFetchMode = iota

	// Hybrid is used to indicate Envoy fetches the JWKs server when there is a cluster entry,
	// otherwise fallback to Istiod
	Hybrid

	// Envoy is used to indicate Envoy ALWAYS fetches the JWKs server
	Envoy
)

// String converts JwksFetchMode to readable string.
func (mode JwksFetchMode) String() string {
	switch mode {
	case Istiod:
		return "Istiod"
	case Hybrid:
		return "Hybrid"
	case Envoy:
		return "Envoy"
	default:
		return "Unset"
	}
}

// ConvertToJwksFetchMode converts from string value mode to enum JwksFetchMode value.
// true and false are kept for backwards compatibility.
func ConvertToJwksFetchMode(mode string) JwksFetchMode {
	switch mode {
	case "istiod", "false":
		return Istiod
	case "hybrid", "true":
		return Hybrid
	case "envoy":
		return Envoy
	default:
		return Istiod
	}
}
