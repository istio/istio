// Copyright 2018 Istio Authors
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

package caclient

import (
	"fmt"
	"strings"

	caClientInterface "istio.io/istio/security/pkg/nodeagent/caclient/interface"
	citadel "istio.io/istio/security/pkg/nodeagent/caclient/providers/citadel"
	gca "istio.io/istio/security/pkg/nodeagent/caclient/providers/google"
)

const googleCAName = "GoogleCA"
const citadelName = "Citadel"

// NewCAClient create an CA client.
func NewCAClient(endpoint, CAProviderName, caTLSRootCertFile string, tlsFlag bool) (caClientInterface.Client, error) {
	switch CAProviderName {
	case googleCAName:
		return gca.NewGoogleCAClient(endpoint, tlsFlag)
	case citadelName:
		return citadel.NewCitadelClient(endpoint, tlsFlag, caTLSRootCertFile)
	default:
		return nil, fmt.Errorf(
			"CA provider %q isn't supported. Currently Istio supports %q", CAProviderName, strings.Join([]string{googleCAName, citadelName}, ","))
	}
}
