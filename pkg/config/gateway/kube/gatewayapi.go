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

package kube

import (
	"fmt"
	"strings"

	"istio.io/istio/pkg/config/constants"
)

// IsInternalGatewayReference returns true if gatewayName is referencing the internal
// Istio Gateway corresponding to a Kubernetes Gateway API gateway.
func IsInternalGatewayReference(gatewayName string) bool {
	parts := strings.SplitN(gatewayName, "/", 2)
	if len(parts) == 2 {
		gatewayName = parts[1]
	}
	return strings.Contains(gatewayName, fmt.Sprintf("~%s~", constants.KubernetesGatewayName))
}

// InternalGatewayName returns the name of the internal Istio Gateway corresponding to the
// specified gateway-api gateway and listener.
func InternalGatewayName(gwName, lName string) string {
	return fmt.Sprintf("%s~%s~%s", gwName, constants.KubernetesGatewayName, lName)
}
