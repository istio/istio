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

package param

// WellKnown defines a well-known template parameter injected automatically by the echo testing framework.
type WellKnown string

const (
	// From is the template parameter used for injecting the source of a call. It will be of type echo.Caller,
	// which is generally either of type echo.Instance or istio.Ingress (for ingress-based tests).
	From WellKnown = "From"

	// To is the template parameter used for injecting the echo.Target of a call.
	To WellKnown = "To"

	// Namespace is the template parameter used for injecting the target namespace.Instance of the applied config.
	Namespace WellKnown = "Namespace"

	// SystemNamespace is the template parameter used for injecting the namespace.Instance of the Istio system.
	SystemNamespace WellKnown = "SystemNamespace"
)

func (p WellKnown) String() string {
	return string(p)
}
