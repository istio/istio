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

package agentgateway

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"

	"github.com/agentgateway/agentgateway/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

const backendTLSPolicySuffix = ":backend-tls"

// dummyCaCert is a sentinel value to send to agentgateway to signal that it should reject TLS connections due to invalid config.
var dummyCaCert = []byte("invalid")

// BackendTLSPolicyInputs holds all the collections needed for BackendTLSPolicy translation.
type BackendTLSPolicyInputs struct {
	BackendTLSPolicies krt.Collection[*gatewayv1.BackendTLSPolicy]
	ConfigMaps         krt.Collection[*corev1.ConfigMap]
	Secrets            krt.Collection[*corev1.Secret]
	Services           krt.Collection[*corev1.Service]
	Gateways           krt.Collection[*gatewayv1.Gateway]
	// AncestorBackends maps backend targets to the gateways that reference them (via routes).
	AncestorBackends krt.Collection[*AncestorBackend]
	ControllerName   string
	DomainSuffix     string
}

// AncestorBackend tracks the relationship between a backend (Service) and the Gateway that references it via a route.
type AncestorBackend struct {
	Gateway types.NamespacedName
	Backend types.NamespacedName
	Source  TypedResource
}

func (a AncestorBackend) ResourceName() string {
	return a.Source.String() + "/" + a.Gateway.String() + "/" + a.Backend.String()
}

func (a AncestorBackend) Equals(other AncestorBackend) bool {
	return a.Gateway == other.Gateway && a.Backend == other.Backend && a.Source == other.Source
}

// BackendTLSPolicyCollection creates a krt collection that translates BackendTLSPolicy resources
// into agentgateway Policy resources with backend TLS configuration.
func BackendTLSPolicyCollection(
	inputs BackendTLSPolicyInputs,
	opts krt.OptionsBuilder,
) (krt.StatusCollection[*gatewayv1.BackendTLSPolicy, gatewayv1.PolicyStatus], krt.Collection[AgwResource]) {
	// Index BackendTLSPolicies by their target references for conflict detection
	backendTLSTargetIndex := krt.NewIndex(inputs.BackendTLSPolicies, "btls-targets", func(o *gatewayv1.BackendTLSPolicy) []string {
		return slices.Map(o.Spec.TargetRefs, func(e gatewayv1.LocalPolicyTargetReferenceWithSectionName) string {
			return fmt.Sprintf("%s/%s/%s", o.Namespace, e.Kind, e.Name)
		})
	})

	// Index AncestorBackends by backend name for lookup
	ancestorIndex := krt.NewIndex(inputs.AncestorBackends, "btls-ancestors", func(o *AncestorBackend) []string {
		return []string{o.Backend.String()}
	})

	return krt.NewStatusManyCollection(inputs.BackendTLSPolicies, func(krtctx krt.HandlerContext, btls *gatewayv1.BackendTLSPolicy) (
		*gatewayv1.PolicyStatus,
		[]AgwResource,
	) {
		return translateBackendTLSPolicy(krtctx, inputs, backendTLSTargetIndex, ancestorIndex, btls)
	}, opts.WithName("BackendTLSPolicies")...)
}

func translateBackendTLSPolicy(
	krtctx krt.HandlerContext,
	inputs BackendTLSPolicyInputs,
	backendTLSTargetIndex krt.Index[string, *gatewayv1.BackendTLSPolicy],
	ancestorIndex krt.Index[string, *AncestorBackend],
	btls *gatewayv1.BackendTLSPolicy,
) (*gatewayv1.PolicyStatus, []AgwResource) {
	var resources []AgwResource
	status := btls.Status.DeepCopy()

	conds := map[string]*Condition{
		string(gatewayv1.PolicyConditionAccepted): {
			reason:  string(gatewayv1.PolicyReasonAccepted),
			message: "Configuration is valid",
		},
		string(gatewayv1.BackendTLSPolicyConditionResolvedRefs): {
			reason:  string(gatewayv1.BackendTLSPolicyReasonResolvedRefs),
			message: "Configuration is valid",
		},
	}

	caCert, err := getBackendTLSCACert(krtctx, inputs.ConfigMaps, btls, conds)
	if err != nil {
		conds[string(gatewayv1.PolicyConditionAccepted)].error = &ConfigError{
			Reason:  string(gatewayv1.BackendTLSPolicyReasonNoValidCACertificate),
			Message: err.Error(),
		}
		caCert = dummyCaCert
	}

	sans := slices.MapFilter(btls.Spec.Validation.SubjectAltNames, func(e gatewayv1.SubjectAltName) *string {
		switch e.Type {
		case gatewayv1.HostnameSubjectAltNameType:
			return ptr.Of(string(e.Hostname))
		case gatewayv1.URISubjectAltNameType:
			return ptr.Of(string(e.URI))
		}
		return nil
	})

	uniqueGateways := sets.New[types.NamespacedName]()
	for _, target := range btls.Spec.TargetRefs {
		tgtKey := fmt.Sprintf("%s/%s/%s", btls.Namespace, target.Kind, target.Name)
		tgtNN := types.NamespacedName{
			Name:      string(target.Name),
			Namespace: btls.Namespace,
		}

		// Find which gateways reference this backend
		ancestors := ancestorIndex.Fetch(krtctx, tgtNN.String())
		for _, a := range ancestors {
			uniqueGateways.Insert(a.Gateway)
		}

		// Check for conflicting policies targeting the same backend
		allPoliciesForTarget := backendTLSTargetIndex.Fetch(krtctx, tgtKey)
		if err := btlsCheckConflicted(btls, target, allPoliciesForTarget); err != nil {
			conds[string(gatewayv1.PolicyConditionAccepted)].error = &ConfigError{
				Reason:  string(gatewayv1.PolicyReasonConflicted),
				Message: err.Error(),
			}
			continue
		}

		// Build the policy target
		var policyTarget *api.PolicyTarget
		serviceHostname := fmt.Sprintf("%s.%s.svc.%s", target.Name, btls.Namespace, inputs.DomainSuffix)
		switch string(target.Kind) {
		case gvk.Service.Kind:
			svcTarget := &api.PolicyTarget_ServiceTarget{
				Namespace: btls.Namespace,
				Hostname:  serviceHostname,
			}
			// Handle named port sectionName
			if sn := target.SectionName; sn != nil {
				_, convErr := strconv.Atoi(string(*sn))
				if convErr != nil {
					// Named port — look up the actual port number
					svc := ptr.Flatten(krt.FetchOne(krtctx, inputs.Services, krt.FilterObjectName(tgtNN)))
					if svc != nil {
						for _, p := range svc.Spec.Ports {
							if p.Name == string(*sn) {
								port := uint32(p.Port) //nolint:gosec
								svcTarget.Port = &port
								break
							}
						}
					}
				}
			}
			policyTarget = &api.PolicyTarget{
				Kind: &api.PolicyTarget_Service{
					Service: svcTarget,
				},
			}
		case gvk.InferencePool.Kind:
			var portNum *uint32
			sn := (*string)(target.SectionName)
			if sn != nil {
				parsed, _ := strconv.Atoi(*sn)
				portNum = ptr.Of(uint32(parsed)) // nolint:gosec // G115: kubebuilder validation ensures safe for uint32
			}
			policyTarget = &api.PolicyTarget{
				Kind: &api.PolicyTarget_Service{
					Service: &api.PolicyTarget_ServiceTarget{
						Namespace: btls.Namespace,
						Hostname:  serviceHostname,
						Port:      portNum,
					},
				},
			}
		default:
			log.Warnf("unsupported BackendTLSPolicy target kind %q", target.Kind)
			continue
		}

		// Build the backend TLS spec
		res := &api.BackendPolicySpec_BackendTLS{
			Root:                  caCert,
			Hostname:              ptr.Of(string(btls.Spec.Validation.Hostname)),
			VerifySubjectAltNames: sans,
		}

		// Check for Gateway-level client certificate (mTLS to backend)
		if uniqueGateways.Len() == 1 {
			gwNN := uniqueGateways.UnsortedList()[0]
			gtw := ptr.Flatten(krt.FetchOne(krtctx, inputs.Gateways, krt.FilterObjectName(gwNN)))
			if gtw != nil && gtw.Spec.TLS != nil && gtw.Spec.TLS.Backend != nil && gtw.Spec.TLS.Backend.ClientCertificateRef != nil {
				clientRef := gtw.Spec.TLS.Backend.ClientCertificateRef
				if clientRef.Namespace == nil && (clientRef.Kind == nil || *clientRef.Kind == "Secret") {
					nn := types.NamespacedName{
						Namespace: gtw.Namespace,
						Name:      string(clientRef.Name),
					}
					scrt := ptr.Flatten(krt.FetchOne(krtctx, inputs.Secrets, krt.FilterObjectName(nn)))
					if scrt != nil {
						if cert, ok := scrt.Data[corev1.TLSCertKey]; ok {
							res.Cert = cert
						}
						if key, ok := scrt.Data[corev1.TLSPrivateKeyKey]; ok {
							res.Key = key
						}
					}
				}
			}
		}

		policyKey := btls.Namespace + "/" + btls.Name + backendTLSPolicySuffix
		if svc := policyTarget.GetService(); svc != nil {
			policyKey += ":" + svc.Namespace + "/" + svc.Hostname
		}

		policy := &api.Policy{
			Key:    policyKey,
			Target: policyTarget,
			Kind: &api.Policy_Backend{
				Backend: &api.BackendPolicySpec{
					Kind: &api.BackendPolicySpec_BackendTls{
						BackendTls: res,
					},
				},
			},
		}

		// Emit for each gateway that references this backend
		gatewayList := uniqueGateways.UnsortedList()
		if len(gatewayList) > 0 {
			for _, gw := range gatewayList {
				resources = append(resources, AgwResource{
					Resource: ToAgwResource(policy),
					Gateway:  gw,
				})
			}
		} else {
			// No known gateways yet — emit unscoped so it applies when routes are added
			resources = append(resources, AgwResource{
				Resource: ToAgwResource(policy),
			})
		}
	}

	// Build ancestor status per gateway
	ancestorStatuses := make([]gatewayv1.PolicyAncestorStatus, 0, uniqueGateways.Len())
	for gw := range uniqueGateways {
		pr := gatewayv1.ParentReference{
			Group: ptr.Of(gatewayv1.Group(gvk.KubernetesGateway.Group)),
			Kind:  ptr.Of(gatewayv1.Kind(gvk.KubernetesGateway.Kind)),
			Name:  gatewayv1.ObjectName(gw.Name),
		}
		ancestorStatuses = append(ancestorStatuses, btlsSetAncestorStatus(
			pr, btls.Generation, conds, gatewayv1.GatewayController(inputs.ControllerName),
		))
	}
	status.Ancestors = btlsMergeAncestors(inputs.ControllerName, status.Ancestors, ancestorStatuses)
	return status, resources
}

// getBackendTLSCACert fetches CA certificate data from ConfigMap references or handles WellKnownCACertificates.
func getBackendTLSCACert(
	krtctx krt.HandlerContext,
	cfgmaps krt.Collection[*corev1.ConfigMap],
	btls *gatewayv1.BackendTLSPolicy,
	conds map[string]*Condition,
) ([]byte, error) {
	validation := btls.Spec.Validation
	if wk := validation.WellKnownCACertificates; wk != nil {
		switch kind := *wk; kind {
		case gatewayv1.WellKnownCACertificatesSystem:
			return nil, nil
		default:
			conds[string(gatewayv1.PolicyConditionAccepted)].error = &ConfigError{
				Reason:  string(gatewayv1.PolicyReasonInvalid),
				Message: fmt.Sprintf("Unknown wellKnownCACertificates: %v", *wk),
			}
			return nil, fmt.Errorf("unknown wellKnownCACertificates: %v", *wk)
		}
	}

	if len(validation.CACertificateRefs) == 0 {
		return nil, fmt.Errorf("no CACertificateRefs specified")
	}

	var sb strings.Builder
	for _, ref := range validation.CACertificateRefs {
		if ref.Group != "" || ref.Kind != "ConfigMap" {
			conds[string(gatewayv1.BackendTLSPolicyConditionResolvedRefs)].error = &ConfigError{
				Reason:  string(gatewayv1.BackendTLSPolicyReasonInvalidKind),
				Message: "Certificate reference invalid: " + string(ref.Kind),
			}
			return nil, fmt.Errorf("invalid certificate reference kind: %v", ref.Kind)
		}
		nn := types.NamespacedName{
			Name:      string(ref.Name),
			Namespace: btls.Namespace,
		}
		cfgmap := krt.FetchOne(krtctx, cfgmaps, krt.FilterObjectName(nn))
		if cfgmap == nil {
			conds[string(gatewayv1.BackendTLSPolicyConditionResolvedRefs)].error = &ConfigError{
				Reason:  string(gatewayv1.BackendTLSPolicyReasonInvalidCACertificateRef),
				Message: "Certificate reference not found",
			}
			return nil, fmt.Errorf("certificate reference not found: %v", nn)
		}
		cm := ptr.Flatten(cfgmap)
		caCert, ok := cm.Data["ca.crt"]
		if !ok {
			conds[string(gatewayv1.BackendTLSPolicyConditionResolvedRefs)].error = &ConfigError{
				Reason:  string(gatewayv1.BackendTLSPolicyReasonInvalidCACertificateRef),
				Message: "ConfigMap missing ca.crt key",
			}
			return nil, fmt.Errorf("ConfigMap %v missing ca.crt key", nn)
		}
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(caCert)
	}
	return []byte(sb.String()), nil
}

// btlsCheckConflicted checks if this BackendTLSPolicy is conflicted by a higher-priority policy targeting the same backend.
func btlsCheckConflicted(
	btls *gatewayv1.BackendTLSPolicy,
	target gatewayv1.LocalPolicyTargetReferenceWithSectionName,
	allPolicies []*gatewayv1.BackendTLSPolicy,
) error {
	for _, m := range allPolicies {
		if m.UID == btls.UID {
			continue
		}
		hasMatchingTarget := false
		for _, t := range m.Spec.TargetRefs {
			if btlsTargetEqual(target, t) {
				hasMatchingTarget = true
				break
			}
		}
		if !hasMatchingTarget {
			continue
		}
		if btlsComparePolicy(m, btls) {
			return fmt.Errorf("policy %v matches the same target but with higher priority", m.Name)
		}
	}
	return nil
}

// btlsComparePolicy returns true if a has higher priority than b.
func btlsComparePolicy(a, b metav1.Object) bool {
	ts := a.GetCreationTimestamp().Compare(b.GetCreationTimestamp().Time)
	if ts < 0 {
		return true
	}
	if ts > 0 {
		return false
	}
	ns := cmp.Compare(a.GetNamespace(), b.GetNamespace())
	if ns < 0 {
		return true
	}
	if ns > 0 {
		return false
	}
	return a.GetName() < b.GetName()
}

func btlsTargetEqual(a, b gatewayv1.LocalPolicyTargetReferenceWithSectionName) bool {
	return a.Group == b.Group &&
		a.Kind == b.Kind &&
		a.Name == b.Name &&
		ptr.Equal(a.SectionName, b.SectionName)
}

// btlsSetAncestorStatus builds a PolicyAncestorStatus for a given gateway.
func btlsSetAncestorStatus(
	pr gatewayv1.ParentReference,
	generation int64,
	conds map[string]*Condition,
	controller gatewayv1.GatewayController,
) gatewayv1.PolicyAncestorStatus {
	conditions := make([]metav1.Condition, 0, len(conds))
	for _, k := range slices.Sort(maps.Keys(conds)) {
		c := conds[k]
		if c.error != nil {
			conditions = append(conditions, metav1.Condition{
				Type:               k,
				Status:             metav1.ConditionFalse,
				ObservedGeneration: generation,
				LastTransitionTime: metav1.Now(),
				Reason:             c.error.Reason,
				Message:            c.error.Message,
			})
		} else {
			conditions = append(conditions, metav1.Condition{
				Type:               k,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: generation,
				LastTransitionTime: metav1.Now(),
				Reason:             c.reason,
				Message:            c.message,
			})
		}
	}
	return gatewayv1.PolicyAncestorStatus{
		AncestorRef:    pr,
		ControllerName: controller,
		Conditions:     conditions,
	}
}

// btlsMergeAncestors merges new ancestor statuses into existing ones, preserving statuses from other controllers.
func btlsMergeAncestors(
	controllerName string,
	existing []gatewayv1.PolicyAncestorStatus,
	newStatuses []gatewayv1.PolicyAncestorStatus,
) []gatewayv1.PolicyAncestorStatus {
	controller := gatewayv1.GatewayController(controllerName)
	// Keep existing statuses from other controllers
	result := make([]gatewayv1.PolicyAncestorStatus, 0, len(existing)+len(newStatuses))
	for _, e := range existing {
		if e.ControllerName != controller {
			result = append(result, e)
		}
	}
	result = append(result, newStatuses...)
	return result
}

// BuildAncestorBackends extracts backend→gateway relationships from routes.
// This is used to determine which gateways reference a given backend Service,
// which is needed for BackendTLSPolicy status reporting.
// TODO(jaellio): support TCPRoute
func BuildAncestorBackends(
	httpRoutes krt.Collection[*gatewayv1.HTTPRoute],
	grpcRoutes krt.Collection[*gatewayv1.GRPCRoute],
	opts krt.OptionsBuilder,
) krt.Collection[*AncestorBackend] {
	httpAncestors := krt.NewManyCollection(httpRoutes, func(_ krt.HandlerContext, obj *gatewayv1.HTTPRoute) []*AncestorBackend {
		source := TypedResource{
			Kind: gvk.HTTPRoute,
			Name: types.NamespacedName{
				Namespace: obj.Namespace,
				Name:      obj.Name,
			},
		}
		gateways := extractGatewayRefs(obj.Namespace, obj.Spec.ParentRefs)
		backends := sets.New[types.NamespacedName]()
		for _, rule := range obj.Spec.Rules {
			for _, ref := range rule.BackendRefs {
				if ref.Kind != nil && *ref.Kind != "Service" {
					continue
				}
				ns := obj.Namespace
				if ref.Namespace != nil {
					ns = string(*ref.Namespace)
				}
				backends.Insert(types.NamespacedName{Namespace: ns, Name: string(ref.Name)})
			}
		}
		return buildAncestorPairs(gateways, backends, source)
	}, opts.WithName("HTTPRouteAncestorBackends")...)

	grpcAncestors := krt.NewManyCollection(grpcRoutes, func(_ krt.HandlerContext, obj *gatewayv1.GRPCRoute) []*AncestorBackend {
		source := TypedResource{
			Kind: gvk.HTTPRoute,
			Name: types.NamespacedName{
				Namespace: obj.Namespace,
				Name:      obj.Name,
			},
		}
		gateways := extractGatewayRefs(obj.Namespace, obj.Spec.ParentRefs)
		backends := sets.New[types.NamespacedName]()
		for _, rule := range obj.Spec.Rules {
			for _, ref := range rule.BackendRefs {
				if ref.Kind != nil && *ref.Kind != "Service" {
					continue
				}
				ns := obj.Namespace
				if ref.Namespace != nil {
					ns = string(*ref.Namespace)
				}
				backends.Insert(types.NamespacedName{Namespace: ns, Name: string(ref.Name)})
			}
		}
		return buildAncestorPairs(gateways, backends, source)
	}, opts.WithName("GRPCRouteAncestorBackends")...)

	return krt.JoinCollection([]krt.Collection[*AncestorBackend]{httpAncestors, grpcAncestors},
		opts.WithName("AllAncestorBackends")...)
}

func extractGatewayRefs(defaultNS string, refs []gatewayv1.ParentReference) sets.Set[types.NamespacedName] {
	gateways := sets.New[types.NamespacedName]()
	for _, r := range refs {
		if r.Group != nil && *r.Group != gatewayv1.GroupName {
			continue
		}
		if r.Kind != nil && *r.Kind != "Gateway" {
			continue
		}
		ns := defaultNS
		if r.Namespace != nil {
			ns = string(*r.Namespace)
		}
		gateways.Insert(types.NamespacedName{Namespace: ns, Name: string(r.Name)})
	}
	return gateways
}

func buildAncestorPairs(gateways, backends sets.Set[types.NamespacedName], source TypedResource) []*AncestorBackend {
	result := make([]*AncestorBackend, 0, gateways.Len()*backends.Len())
	for gw := range gateways {
		for be := range backends {
			result = append(result, &AncestorBackend{Source: source, Gateway: gw, Backend: be})
		}
	}
	return result
}
