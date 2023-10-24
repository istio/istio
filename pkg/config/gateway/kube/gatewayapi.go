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
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/log"
)

// IsK8sGatewayReference returns true if gatewayName is referencing a Kubernetes gateway.
// A Kubernetes Gateway API gateway is reference with the following syntax:
//
//	[ namespace "/" ] "gateway.networking.k8s.io:" gateway-name "." listener-name
//
// Example references:
// "example-ns/gateway.networking.k8s.io:example-gateway.default"
// "gateway.networking.k8s.io:example-gateway.default"
func IsK8sGatewayReference(gatewayName string) bool {
	parts := strings.SplitN(gatewayName, "/", 2)
	if len(parts) == 2 {
		gatewayName = parts[1]
	}
	return strings.HasPrefix(gatewayName, fmt.Sprintf("%s:", gvk.KubernetesGateway.Group))
}

// GatewayToInternalName converts a Kubernetes Gateway reference to its corresponding Istio Gateway.
// Note that both a Gateway and listener name must be specified in a Kubernetes Gateway reference.
func GatewayToInternalName(gwName string) string {
	name, isK8s := strings.CutPrefix(gwName, fmt.Sprintf("%s:", gvk.KubernetesGateway.Group))
	if !isK8s {
		return gwName
	}
	gw, l, ok := strings.Cut(name, ".")
	if ok {
		return InternalGatewayName(gw, l)
	}
	log.Errorf("invalid gateway listener ref: %s", gwName)
	return gwName
}

// IsInternalGatewayReference returns true if gatewayName is referencing the internal
// Istio Gateway corresponding to a Kubernetes Gateway API gateway.
func IsInternalGatewayReference(gatewayName string) bool {
	parts := strings.SplitN(gatewayName, "/", 2)
	if len(parts) == 2 {
		gatewayName = parts[1]
	}
	return strings.Contains(gatewayName, fmt.Sprintf("-%s-", constants.KubernetesGatewayName))
}

// InternalGatewayName returns the name of the internal Istio Gateway corresponding to the
// specified gateway-api gateway and listener.
func InternalGatewayName(gwName, lName string) string {
	return fmt.Sprintf("%s-%s-%s", gwName, constants.KubernetesGatewayName, lName)
}

// GatewayFromInternalName returns the Kubernetes gateway and listener name corresponding to
// the specified internal Istio Gateway.
func GatewayFromInternalName(name string) (gwName string, lName string) {
	gwName, lName, _ = strings.Cut(name, fmt.Sprintf("-%s-", constants.KubernetesGatewayName))
	return
}
