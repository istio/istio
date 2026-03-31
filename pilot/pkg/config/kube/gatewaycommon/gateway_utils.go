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

package gatewaycommon

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/features"
)

const (
	// GatewayClassDefaults is the label key for gateway class defaults on ConfigMaps.
	GatewayClassDefaults = "gateway.istio.io/defaults-for-class"
	// GatewayTLSTerminateModeKey is the annotation key for TLS terminate mode.
	GatewayTLSTerminateModeKey = "gateway.istio.io/tls-terminate-mode"
)

// GetDefaultName returns the default name for a gateway resource.
func GetDefaultName(name string, kgw *gatewayv1.GatewaySpec, disableNameSuffix bool) string {
	if disableNameSuffix {
		return name
	}
	return fmt.Sprintf("%v-%v", name, kgw.GatewayClassName)
}

// IsManaged checks if a Gateway is managed (ie we create the Deployment and Service) or unmanaged.
// This is based on the address field of the spec. If address is set with a Hostname type, it should point to an existing
// Service that handles the gateway traffic. If it is not set, or refers to only a single IP, we will consider it managed and provision the Service.
// If there is an IP, we will set the `loadBalancerIP` type.
// While there is no defined standard for this in the API yet, it is tracked in https://github.com/kubernetes-sigs/gateway-api/issues/892.
// So far, this mirrors how out of clusters work (address set means to use existing IP, unset means to provision one),
// and there has been growing consensus on this model for in cluster deployments.
//
// Currently, the supported options are:
// * 1 Hostname value. This can be short Service name ingress, or FQDN ingress.ns.svc.cluster.local, example.com. If its a non-k8s FQDN it is a ServiceEntry.
// * 1 IP address. This is managed, with IP explicit
// * Nothing. This is managed, with IP auto assigned
//
// Not supported:
// Multiple hostname/IP - It is feasible but preference is to create multiple Gateways. This would also break the 1:1 mapping of GW:Service
// Mixed hostname and IP - doesn't make sense; user should define the IP in service
// NamedAddress - Service has no concept of named address. For cloud's that have named addresses they can be configured by annotations,
//
//	which users can add to the Gateway.
//
// If manual deployments are disabled, IsManaged() always returns true.
func IsManaged(gw *gatewayv1.GatewaySpec) bool {
	if !features.EnableGatewayAPIManualDeployment {
		return true
	}
	if len(gw.Addresses) == 0 {
		return true
	}
	if len(gw.Addresses) > 1 {
		return false
	}
	if t := gw.Addresses[0].Type; t == nil || *t == gatewayv1.IPAddressType {
		return true
	}
	return false
}

// NamespaceNameLabel represents that label added automatically to namespaces is newer Kubernetes clusters
const NamespaceNameLabel = "kubernetes.io/metadata.name"

// NamespaceAcceptedByAllowListeners determines whether a namespace is accepted by a Gateway's AllowedListeners.
func NamespaceAcceptedByAllowListeners(localNamespace string, parent *gatewayv1.Gateway, lookupNamespace func(string) *corev1.Namespace) bool {
	lr := parent.Spec.AllowedListeners
	// Default allows none
	if lr == nil || lr.Namespaces == nil {
		return false
	}
	n := *lr.Namespaces
	if n.From != nil {
		switch *n.From {
		case gatewayv1.NamespacesFromAll:
			return true
		case gatewayv1.NamespacesFromSame:
			return localNamespace == parent.Namespace
		case gatewayv1.NamespacesFromNone:
			return false
		case gatewayv1.NamespacesFromSelector:
			// Fallthrough
		default:
			// Unknown?
			return false
		}
	}
	if lr.Namespaces.Selector == nil {
		// Should never happen, invalid config
		return false
	}
	ls, err := metav1.LabelSelectorAsSelector(lr.Namespaces.Selector)
	if err != nil {
		return false
	}
	localNamespaceObject := lookupNamespace(localNamespace)
	if localNamespaceObject == nil {
		// Couldn't find the namespace
		return false
	}
	return ls.Matches(toNamespaceSet(localNamespaceObject.Name, localNamespaceObject.Labels))
}

// toNamespaceSet converts a set of namespace labels to a Set that can be used to select against.
func toNamespaceSet(name string, labels map[string]string) klabels.Set {
	// If namespace label is not set, implicitly insert it to support older Kubernetes versions
	if labels[NamespaceNameLabel] == name {
		// Already set, avoid copies
		return labels
	}
	// First we need a copy to not modify the underlying object
	ret := make(map[string]string, len(labels)+1)
	for k, v := range labels {
		ret[k] = v
	}
	ret[NamespaceNameLabel] = name
	return ret
}

// ConvertListenerSetToListener converts a ListenerEntry to a standard Listener.
func ConvertListenerSetToListener(l gatewayv1.ListenerEntry) gatewayv1.Listener {
	// For now, structs are identical enough Go can cast them. I doubt this will hold up forever, but we can adjust as needed.
	return gatewayv1.Listener(l)
}
