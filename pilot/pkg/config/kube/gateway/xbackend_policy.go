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

package gateway

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pilot/pkg/features"
	creds "istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
)

// XBackendConnectionPolicyResult is the result of compiling the connection
// policy for one consumer-aware binding. Invalid results are retained so the
// same graph can drive XBackend status and route diagnostics.
type XBackendConnectionPolicyResult struct {
	Binding         BackendBinding
	DestinationRule *config.Config
	Error           string
}

func (r XBackendConnectionPolicyResult) ResourceName() string {
	return r.Binding.ResourceName()
}

func (r XBackendConnectionPolicyResult) Equals(other XBackendConnectionPolicyResult) bool {
	return r.Binding.Equals(other.Binding) && r.Error == other.Error && reflect.DeepEqual(r.DestinationRule, other.DestinationRule)
}

type XBackendConnectionPolicyCollections struct {
	Results          krt.Collection[XBackendConnectionPolicyResult]
	ValidBindings    krt.Collection[BackendBinding]
	DestinationRules krt.Collection[config.Config]
}

// XBackendConnectionPolicies compiles inline TLS and provides a fail-closed
// binding projection. Consumers must use ValidBindings, rather than Active,
// for runtime service publication.
func XBackendConnectionPolicies(
	bindings BackendBindingCollections,
	references *gatewaycommon.ReferenceSet,
	grants gatewaycommon.ReferenceGrants,
	opts krt.OptionsBuilder,
) XBackendConnectionPolicyCollections {
	results := krt.NewCollection(bindings.Active, func(ctx krt.HandlerContext, binding BackendBinding) *XBackendConnectionPolicyResult {
		dr, err := compileXBackendConnectionPolicy(ctx, binding, references, grants)
		result := XBackendConnectionPolicyResult{Binding: binding, DestinationRule: dr}
		if err != nil {
			result.Error = err.Error()
		}
		return &result
	}, opts.WithName("XBackendConnectionPolicyResults")...)

	valid := krt.NewCollection(results, func(_ krt.HandlerContext, result XBackendConnectionPolicyResult) *BackendBinding {
		if result.Error != "" {
			return nil
		}
		return &result.Binding
	}, opts.WithName("ValidXBackendBindings")...)

	byHost := krt.NewIndex(results, "xbackend-policy-host", func(result XBackendConnectionPolicyResult) []string {
		if result.Error != "" || result.DestinationRule == nil {
			return nil
		}
		return []string{string(result.Binding.InternalName)}
	})
	destinationRules := krt.NewCollection(byHost.AsCollection(opts.WithName("XBackendConnectionPolicyByHost")...),
		func(_ krt.HandlerContext, item krt.IndexObject[string, XBackendConnectionPolicyResult]) *config.Config {
			// All v1.6 binding edges for one opaque hostname have identical policy.
			// Pick deterministically for defense in depth.
			ordered := slices.SortFunc(item.Objects, func(a, b XBackendConnectionPolicyResult) int {
				return strings.Compare(a.Binding.ResourceName(), b.Binding.ResourceName())
			})
			copy := ordered[0].DestinationRule.DeepCopy()
			return &copy
		}, opts.WithName("XBackendDestinationRules")...)

	return XBackendConnectionPolicyCollections{Results: results, ValidBindings: valid, DestinationRules: destinationRules}
}

func compileXBackendConnectionPolicy(
	ctx krt.HandlerContext,
	binding BackendBinding,
	references *gatewaycommon.ReferenceSet,
	_ gatewaycommon.ReferenceGrants,
) (*config.Config, error) {
	if binding.Protocol == gatewayx.BackendProtocolMCP {
		return nil, fmt.Errorf("unsupported XBackend protocol %q: Istio has no MCP backend primitive", binding.Protocol)
	}
	if binding.TLS == nil || binding.TLS.Mode == gatewayx.BackendTLSModeNone {
		return nil, nil
	}

	clientTLS := &networking.ClientTLSSettings{
		Mode: networking.ClientTLSSettings_SIMPLE,
		Sni:  string(binding.TLS.Validation.Hostname),
	}
	if clientTLS.Sni == "" {
		clientTLS.Sni = binding.Endpoint.Hostname
	}
	conds := map[string]*condition{
		string(gatewayv1.PolicyConditionAccepted):               {},
		string(gatewayv1.BackendTLSPolicyConditionResolvedRefs): {},
	}
	clientTLS.SubjectAltNames = slices.MapFilter(binding.TLS.Validation.SubjectAltNames, func(e gatewayv1.SubjectAltName) *string {
		switch e.Type {
		case gatewayv1.HostnameSubjectAltNameType:
			return ptr.Of(string(e.Hostname))
		case gatewayv1.URISubjectAltNameType:
			return ptr.Of(string(e.URI))
		default:
			return nil
		}
	})
	clientTLS.CredentialName = getBackendTLSCredentialName(ctx, binding.TLS.Validation, binding.Namespace, conds, references)
	for _, conditionType := range []string{
		string(gatewayv1.PolicyConditionAccepted),
		string(gatewayv1.BackendTLSPolicyConditionResolvedRefs),
	} {
		if conds[conditionType].error != nil {
			return nil, fmt.Errorf("invalid XBackend TLS validation: %s", conds[conditionType].error.Message)
		}
	}
	if clientTLS.CredentialName == creds.InvalidSecretTypeURI {
		return nil, fmt.Errorf("invalid XBackend CA certificate reference")
	}

	switch binding.TLS.Mode {
	case gatewayx.BackendTLSModeServerOnly:
		// SIMPLE above is complete.
	case gatewayx.BackendTLSModeClientAndServer:
		// DestinationRule has one credentialName for both the client identity and
		// validation context. XBackend deliberately has independent client and CA
		// references, so mapping either one would silently discard the other.
		return nil, fmt.Errorf("unsupported XBackend TLS mode %q: Istio cannot represent separate client and CA credentials", binding.TLS.Mode)
	default:
		return nil, fmt.Errorf("unsupported XBackend TLS mode %q", binding.TLS.Mode)
	}

	return &config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.DestinationRule,
			Name:             generateDRName(binding.Source, string(binding.InternalName)),
			Namespace:        binding.Namespace,
			Annotations: map[string]string{
				constants.InternalParentNames: binding.Source.Kind.String() + "/" + binding.Source.Namespace + "." + binding.Source.Name,
			},
		},
		Spec: &networking.DestinationRule{
			Host: string(binding.InternalName),
			TrafficPolicy: &networking.TrafficPolicy{
				Tls: clientTLS,
			},
		},
	}, nil
}

// XBackendStatusCollection derives Istio-owned ancestor entries from the same
// binding and policy results used for runtime activation.
func XBackendStatusCollection(
	backends krt.Collection[*gatewayx.XBackend],
	bindings BackendBindingCollections,
	policies krt.Collection[XBackendConnectionPolicyResult],
	opts krt.OptionsBuilder,
) krt.StatusCollection[*gatewayx.XBackend, gatewayx.BackendStatus] {
	bindingIndex := krt.NewIndex(bindings.Results, "xbackend-status-source", func(result BackendBindingResult) []TypedNamespacedName {
		return []TypedNamespacedName{result.Source}
	})
	policyIndex := krt.NewIndex(policies, "xbackend-status-policy-source", func(result XBackendConnectionPolicyResult) []TypedNamespacedName {
		return []TypedNamespacedName{result.Binding.Source}
	})
	statusCollection, _ := krt.NewStatusCollection(backends,
		func(ctx krt.HandlerContext, backend *gatewayx.XBackend) (*gatewayx.BackendStatus, *struct{}) {
			source := TypedNamespacedName{NamespacedName: config.NamespacedName(backend), Kind: kind.XBackend}
			status := xBackendStatus(backend, bindingIndex.Fetch(ctx, source), policyIndex.Fetch(ctx, source))
			return &status, nil
		}, opts.WithName("XBackendStatus")...)
	return statusCollection
}

func xBackendStatus(
	backend *gatewayx.XBackend,
	bindingResults []BackendBindingResult,
	policyResults []XBackendConnectionPolicyResult,
) gatewayx.BackendStatus {
	type aggregate struct {
		valid  bool
		errors []string
	}
	byGateway := map[types.NamespacedName]*aggregate{}
	for _, result := range bindingResults {
		a := byGateway[result.Gateway]
		if a == nil {
			a = &aggregate{}
			byGateway[result.Gateway] = a
		}
		if result.Binding != nil {
			a.valid = true
		} else if result.Error != "" {
			a.errors = append(a.errors, result.Error)
		}
	}
	for _, result := range policyResults {
		a := byGateway[result.Binding.Gateway]
		if a == nil {
			a = &aggregate{}
			byGateway[result.Binding.Gateway] = a
		}
		if result.Error != "" {
			a.valid = false
			a.errors = append(a.errors, result.Error)
		}
	}

	gateways := make([]types.NamespacedName, 0, len(byGateway))
	for gateway := range byGateway {
		gateways = append(gateways, gateway)
	}
	sort.Slice(gateways, func(i, j int) bool {
		if gateways[i].Namespace != gateways[j].Namespace {
			return gateways[i].Namespace < gateways[j].Namespace
		}
		return gateways[i].Name < gateways[j].Name
	})

	controller := gatewayv1.GatewayController(features.ManagedGatewayController)
	incoming := make([]gatewayx.BackendAncestorStatus, 0, len(gateways))
	for _, gateway := range gateways {
		a := byGateway[gateway]
		ref := gatewayParentReference(gateway, backend.Namespace)
		current := slices.FindFunc(backend.Status.Ancestors, func(existing gatewayx.BackendAncestorStatus) bool {
			return existing.ControllerName == controller && parentRefEqual(existing.AncestorRef, ref)
		})
		var currentConditions []metav1.Condition
		if current != nil {
			currentConditions = current.Conditions
		}
		condition := &gatewaycommon.ListenerStatusCondition{
			Status:  metav1.ConditionTrue,
			Reason:  "Accepted",
			Message: "XBackend connection configuration is valid",
		}
		if !a.valid {
			condition.Status = metav1.ConditionFalse
			condition.Reason = "Invalid"
			condition.Message = strings.Join(a.errors, "; ")
			if condition.Message == "" {
				condition.Message = "XBackend has no valid attached Route reference"
			}
		}
		incoming = append(incoming, gatewayx.BackendAncestorStatus{
			ControllerName: controller,
			AncestorRef:    ref,
			Conditions: gatewaycommon.SetResourceConditions(backend.Generation, currentConditions,
				map[string]*gatewaycommon.ListenerStatusCondition{"Accepted": condition}),
		})
	}

	ancestors := make([]gatewayx.BackendAncestorStatus, 0, len(backend.Status.Ancestors)+len(incoming))
	for _, existing := range backend.Status.Ancestors {
		if existing.ControllerName != controller {
			ancestors = append(ancestors, existing)
		}
	}
	ancestors = append(ancestors, incoming...)
	return gatewayx.BackendStatus{Ancestors: ancestors[:min(len(ancestors), 32)]}
}

func gatewayParentReference(gateway types.NamespacedName, backendNamespace string) gatewayv1.ParentReference {
	group := gatewayv1.Group(gvk.KubernetesGateway.Group)
	k := gatewayv1.Kind(gvk.KubernetesGateway.Kind)
	ref := gatewayv1.ParentReference{Group: &group, Kind: &k, Name: gatewayv1.ObjectName(gateway.Name)}
	if gateway.Namespace != backendNamespace {
		namespace := gatewayv1.Namespace(gateway.Namespace)
		ref.Namespace = &namespace
	}
	return ref
}
