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
	"cmp"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/wrapperspb"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gw "sigs.k8s.io/gateway-api/apis/v1"
	gatewayx "sigs.k8s.io/gateway-api/apisx/v1alpha1"

	networking "istio.io/api/networking/v1alpha3"
	kubesecrets "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/status"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	schematypes "istio.io/istio/pkg/config/schema/kubetypes"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/sets"
)

type TypedNamespacedName struct {
	types.NamespacedName
	Kind kind.Kind
}

func (n TypedNamespacedName) String() string {
	return n.Kind.String() + "/" + n.NamespacedName.String()
}

type BackendPolicy struct {
	Source       TypedNamespacedName
	TargetIndex  int
	Target       TypedNamespacedName
	SectionName  *string
	TLS          *networking.ClientTLSSettings
	LoadBalancer *networking.LoadBalancerSettings
	RetryBudget  *networking.TrafficPolicy_RetryBudget
	CreationTime time.Time
}

func (b BackendPolicy) ResourceName() string {
	return b.Source.String() + "/" + fmt.Sprint(b.TargetIndex)
}

var TypedNamespacedNameIndexCollectionFunc = krt.WithIndexCollectionFromString(func(s string) TypedNamespacedName {
	parts := strings.Split(s, "/")
	if len(parts) != 3 {
		panic("invalid TypedNamespacedName: " + s)
	}
	return TypedNamespacedName{
		NamespacedName: types.NamespacedName{
			Namespace: parts[1],
			Name:      parts[2],
		},
		Kind: kind.FromString(parts[0]),
	}
})

func (b BackendPolicy) Equals(other BackendPolicy) bool {
	return b.Source == other.Source &&
		ptr.Equal(b.SectionName, other.SectionName) &&
		protoconv.Equals(b.TLS, other.TLS) &&
		protoconv.Equals(b.LoadBalancer, other.LoadBalancer) &&
		protoconv.Equals(b.RetryBudget, other.RetryBudget)
}

// DestinationRuleCollection returns a collection of DestinationRule objects. These are built from a few different
// policy types that are merged together.
func DestinationRuleCollection(
	trafficPolicies krt.Collection[*gatewayx.XBackendTrafficPolicy],
	tlsPolicies krt.Collection[*gw.BackendTLSPolicy],
	ancestors krt.Index[TypedNamespacedName, AncestorBackend],
	references *ReferenceSet,
	domainSuffix string,
	c *Controller,
	services krt.Collection[*v1.Service],
	opts krt.OptionsBuilder,
) krt.Collection[*config.Config] {
	trafficPolicyStatus, backendTrafficPolicies := BackendTrafficPolicyCollection(trafficPolicies, references, opts)
	status.RegisterStatus(c.status, trafficPolicyStatus, GetStatus)
	ancestorCollection := ancestors.AsCollection(append(opts.WithName("AncestorBackend"), TypedNamespacedNameIndexCollectionFunc)...)

	tlsPolicyStatus, backendTLSPolicies := BackendTLSPolicyCollection(tlsPolicies, ancestorCollection, references, opts)
	status.RegisterStatus(c.status, tlsPolicyStatus, GetStatus)

	// We need to merge these by hostname into a single DR
	allPolicies := krt.JoinCollection([]krt.Collection[BackendPolicy]{backendTrafficPolicies, backendTLSPolicies})
	byTarget := krt.NewIndex(allPolicies, "target", func(o BackendPolicy) []TypedNamespacedName {
		return []TypedNamespacedName{o.Target}
	})
	indexOpts := append(opts.WithName("BackendPolicyByTarget"), TypedNamespacedNameIndexCollectionFunc)
	merged := krt.NewCollection(
		byTarget.AsCollection(indexOpts...),
		func(ctx krt.HandlerContext, i krt.IndexObject[TypedNamespacedName, BackendPolicy]) **config.Config {
			svc := i.Key
			// Sort so we can pick the oldest, which will win.
			// Not yet standardized but likely will be (https://github.com/kubernetes-sigs/gateway-api/issues/3516#issuecomment-2684039692)
			pols := slices.SortFunc(i.Objects, func(a, b BackendPolicy) int {
				if r := a.CreationTime.Compare(b.CreationTime); r != 0 {
					return r
				}
				if r := cmp.Compare(a.Source.Namespace, b.Source.Namespace); r != 0 {
					return r
				}
				return cmp.Compare(a.Source.Name, b.Source.Name)
			})
			tlsSet := false
			lbSet := false
			rbSet := false
			spec := &networking.DestinationRule{
				Host:          svc.Name + "." + svc.Namespace + ".svc." + domainSuffix, // "%s.%s.svc.%v"
				TrafficPolicy: &networking.TrafficPolicy{},
			}
			portLevelSettings := make(map[string]*networking.TrafficPolicy_PortTrafficPolicy)
			parents := make([]string, 0, len(pols))
			for _, pol := range pols {
				if pol.TLS != nil {
					if pol.SectionName != nil {
						// Port-specific TLS setting
						portName := *pol.SectionName
						if _, exists := portLevelSettings[portName]; !exists {
							portLevelSettings[portName] = &networking.TrafficPolicy_PortTrafficPolicy{
								Port: &networking.PortSelector{Number: 0}, // Will be resolved later
								Tls:  pol.TLS,
							}
						}
					} else {
						// Service-wide TLS setting
						if tlsSet {
							// We only allow 1. TODO: report status if there are multiple
							continue
						}
						tlsSet = true
						spec.TrafficPolicy.Tls = pol.TLS
					}
				}
				if pol.LoadBalancer != nil {
					if lbSet {
						// We only allow 1. TODO: report status if there are multiple
						continue
					}
					lbSet = true
					spec.TrafficPolicy.LoadBalancer = pol.LoadBalancer
				}
				if pol.RetryBudget != nil {
					if rbSet {
						// We only allow 1. TODO: report status if there are multiple
						continue
					}
					rbSet = true
					spec.TrafficPolicy.RetryBudget = pol.RetryBudget
				}
				parentName := pol.Source.Kind.String() + "/" + pol.Source.Namespace + "." + pol.Source.Name
				if !slices.Contains(parents, parentName) {
					parents = append(parents, parentName)
				}
			}

			if len(portLevelSettings) > 0 {
				serviceKey := svc.Namespace + "/" + svc.Name
				service := ptr.Flatten(krt.FetchOne(ctx, services, krt.FilterKey(serviceKey)))

				for portName, portPolicy := range portLevelSettings {
					if service != nil {
						for _, port := range service.Spec.Ports {
							if port.Name == portName {
								portPolicy.Port = &networking.PortSelector{Number: uint32(port.Port)}
								break
							}
						}
					}
					spec.TrafficPolicy.PortLevelSettings = append(spec.TrafficPolicy.PortLevelSettings, portPolicy)
				}
			}

			cfg := &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             svc.Name + "~" + constants.KubernetesGatewayName,
					Namespace:        svc.Namespace,
					Annotations: map[string]string{
						constants.InternalParentNames: strings.Join(parents, ","),
					},
				},
				Spec: spec,
			}
			return &cfg
		}, opts.WithName("BackendPolicyMerged")...)
	return merged
}

func BackendTLSPolicyCollection(
	tlsPolicies krt.Collection[*gw.BackendTLSPolicy],
	ancestors krt.IndexCollection[TypedNamespacedName, AncestorBackend],
	references *ReferenceSet,
	opts krt.OptionsBuilder,
) (krt.StatusCollection[*gw.BackendTLSPolicy, gw.PolicyStatus], krt.Collection[BackendPolicy]) {
	return krt.NewStatusManyCollection(tlsPolicies, func(ctx krt.HandlerContext, i *gw.BackendTLSPolicy) (
		*gw.PolicyStatus,
		[]BackendPolicy,
	) {
		status := i.Status.DeepCopy()
		res := make([]BackendPolicy, 0, len(i.Spec.TargetRefs))

		tls := &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE}
		s := i.Spec

		conds := map[string]*condition{
			string(gw.PolicyConditionAccepted): {
				reason:  string(gw.PolicyReasonAccepted),
				message: "Configuration is valid",
			},
			string(gw.BackendTLSPolicyConditionResolvedRefs): {
				reason:  string(gw.BackendTLSPolicyReasonResolvedRefs),
				message: "Configuration is valid",
			},
		}
		tls.Sni = string(s.Validation.Hostname)
		tls.SubjectAltNames = slices.MapFilter(s.Validation.SubjectAltNames, func(e gw.SubjectAltName) *string {
			switch e.Type {
			case gw.HostnameSubjectAltNameType:
				return ptr.Of(string(e.Hostname))
			case gw.URISubjectAltNameType:
				return ptr.Of(string(e.URI))
			}
			return nil
		})
		tls.CredentialName = getBackendTLSCredentialName(s.Validation, i.Namespace, conds, references)

		// In ancestor status, we need to report for Service (for mesh) and for each relevant Gateway.
		// However, there is a max of 16 items we can report.
		// Reporting per-Gateway has no value (perhaps for anyone, but certainly not for Istio), so we favor the Service attachments
		// getting to take the 16 slots.
		// The Gateway API spec says that if there are more than 16, the policy should not be applied. This is a terrible, anti-user, decision
		// that Istio will not follow, even if it means failing conformance tests.
		ancestorStatus := make([]gw.PolicyAncestorStatus, 0, len(i.Spec.TargetRefs))
		uniqueGateways := sets.New[types.NamespacedName]()
		for idx, t := range i.Spec.TargetRefs {
			conds = maps.Clone(conds)
			refo, err := references.LocalPolicyTargetRef(t.LocalPolicyTargetReference, i.Namespace)
			var sectionName *string
			if err == nil {
				switch refType := refo.(type) {
				case *v1.Service:
					if t.SectionName != nil && *t.SectionName != "" {
						sectionName = ptr.Of(string(*t.SectionName))
						portExists := false
						for _, port := range refType.Spec.Ports {
							if port.Name == *sectionName {
								portExists = true
								break
							}
						}
						if !portExists {
							err = fmt.Errorf("sectionName %q does not exist in Service %s/%s", *sectionName, refType.Namespace, refType.Name)
						}
					}
				default:
					err = fmt.Errorf("unsupported reference kind: %v", t.Kind)
				}
			}
			if err != nil {
				conds[string(gw.PolicyConditionAccepted)].error = &ConfigError{
					Reason:  string(gw.PolicyReasonTargetNotFound),
					Message: "targetRefs invalid: " + err.Error(),
				}
			} else {
				target := TypedNamespacedName{
					NamespacedName: types.NamespacedName{
						Name:      string(t.Name),
						Namespace: i.Namespace,
					},
					Kind: gvk.MustToKind(schematypes.GvkFromObject(refo.(controllers.Object))),
				}
				// Only create an object if we can resolve the target
				res = append(res, BackendPolicy{
					Source: TypedNamespacedName{
						NamespacedName: config.NamespacedName(i),
						Kind:           kind.BackendTLSPolicy,
					},
					TargetIndex:  idx,
					Target:       target,
					SectionName:  sectionName,
					TLS:          tls,
					CreationTime: i.CreationTimestamp.Time,
				})
				log.Errorf("howardjohn: TARGET %v", target)
				ancestorBackends := krt.Fetch(ctx, ancestors, krt.FilterKey(target.String()))
				for _, gwl := range ancestorBackends {
					for _, i := range gwl.Objects {
						uniqueGateways.Insert(i.Gateway)
					}
				}
			}
			// We add a status for Service (for mesh), and for each Gateway
			meshPR := gw.ParentReference{
				Group:       &t.Group,
				Kind:        &t.Kind,
				Name:        t.Name,
				SectionName: t.SectionName,
			}
			ancestorStatus = append(ancestorStatus, setAncestorStatus(meshPR, status, i.Generation, conds, constants.ManagedGatewayMeshController))
		}
		gwl := slices.SortBy(uniqueGateways.UnsortedList(), types.NamespacedName.String)
		for _, g := range gwl {
			pr := gw.ParentReference{
				Group: ptr.Of(gw.Group(gvk.KubernetesGateway.Group)),
				Kind:  ptr.Of(gw.Kind(gvk.KubernetesGateway.Kind)),
				Name:  gw.ObjectName(g.Name),
			}
			ancestorStatus = append(ancestorStatus, setAncestorStatus(pr, status, i.Generation, conds, gw.GatewayController(features.ManagedGatewayController)))
		}
		log.Errorf("howardjohn: set ancestors %v for %v, existing %v", config.NamespacedName(i), len(ancestorStatus), len(status.Ancestors))
		status.Ancestors = mergeAncestors(status.Ancestors, ancestorStatus)
		return status, res
	}, opts.WithName("BackendTLSPolicy")...)
}

func getBackendTLSCredentialName(
	validation gw.BackendTLSPolicyValidation,
	policyNamespace string,
	conds map[string]*condition,
	references *ReferenceSet,
) string {
	if wk := validation.WellKnownCACertificates; wk != nil {
		switch *wk {
		case gw.WellKnownCACertificatesSystem:
			// Already our default, no action needed
		default:
			conds[string(gw.PolicyConditionAccepted)].error = &ConfigError{
				Reason:  string(gw.PolicyReasonInvalid),
				Message: fmt.Sprintf("Unknown wellKnownCACertificates: %v", *wk),
			}
		}
		return ""
	}
	if len(validation.CACertificateRefs) == 0 {
		return ""
	}

	// Spec should require but double check
	// We only support 1
	ref := validation.CACertificateRefs[0]
	if len(validation.CACertificateRefs) > 1 {
		conds[string(gw.PolicyConditionAccepted)].message += "; warning: only the first caCertificateRefs will be used"
	}
	refo, err := references.LocalPolicyRef(ref, policyNamespace)
	if err == nil {
		switch to := refo.(type) {
		case *v1.ConfigMap:
			if _, rerr := kubesecrets.ExtractRootFromString(to.Data); rerr != nil {
				err = rerr
				conds[string(gw.BackendTLSPolicyReasonResolvedRefs)].error = &ConfigError{
					Reason:  string(gw.BackendTLSPolicyReasonInvalidCACertificateRef),
					Message: "Certificate invalid: " + err.Error(),
				}
			} else {
				return credentials.KubernetesConfigMapTypeURI + policyNamespace + "/" + string(ref.Name)
			}
		// TODO: for now we do not support Secret references.
		// Core requires only ConfigMap
		// We can do so, we just need to make it so this propagates through to SecretAllowed, otherwise clients in other namespaces
		// will not be given access.
		// Additionally, we will need to ensure we don't accidentally authorize them to access the private key, just the ca.crt
		default:
			err = fmt.Errorf("unsupported reference kind: %v", ref.Kind)
			conds[string(gw.BackendTLSPolicyReasonResolvedRefs)].error = &ConfigError{
				Reason:  string(gw.BackendTLSPolicyReasonInvalidKind),
				Message: "Certificate reference invalid: " + err.Error(),
			}
		}
	} else {
		if strings.Contains(err.Error(), "unsupported kind") {
			conds[string(gw.BackendTLSPolicyReasonResolvedRefs)].error = &ConfigError{
				Reason:  string(gw.BackendTLSPolicyReasonInvalidKind),
				Message: "Certificate reference not supported: " + err.Error(),
			}
		} else {
			conds[string(gw.BackendTLSPolicyReasonResolvedRefs)].error = &ConfigError{
				Reason:  string(gw.BackendTLSPolicyReasonInvalidCACertificateRef),
				Message: "Certificate reference not found: " + err.Error(),
			}
		}
	}
	if err != nil {
		conds[string(gw.PolicyConditionAccepted)].error = &ConfigError{
			Reason:  string(gw.BackendTLSPolicyReasonNoValidCACertificate),
			Message: "Certificate reference invalid: " + err.Error(),
		}
		// Generate an invalid reference. This ensures traffic is blocked.
		// See https://github.com/kubernetes-sigs/gateway-api/issues/3516 for upstream clarification on desired behavior here.
		return credentials.InvalidSecretTypeURI
	}
	return ""
}

func BackendTrafficPolicyCollection(
	trafficPolicies krt.Collection[*gatewayx.XBackendTrafficPolicy],
	references *ReferenceSet,
	opts krt.OptionsBuilder,
) (krt.StatusCollection[*gatewayx.XBackendTrafficPolicy, gatewayx.PolicyStatus], krt.Collection[BackendPolicy]) {
	return krt.NewStatusManyCollection(trafficPolicies, func(ctx krt.HandlerContext, i *gatewayx.XBackendTrafficPolicy) (
		*gatewayx.PolicyStatus,
		[]BackendPolicy,
	) {
		status := i.Status.DeepCopy()
		res := make([]BackendPolicy, 0, len(i.Spec.TargetRefs))
		ancestors := make([]gw.PolicyAncestorStatus, 0, len(i.Spec.TargetRefs))

		lb := &networking.LoadBalancerSettings{}
		var retryBudget *networking.TrafficPolicy_RetryBudget

		conds := map[string]*condition{
			string(gw.PolicyConditionAccepted): {
				reason:  string(gw.PolicyReasonAccepted),
				message: "Configuration is valid",
			},
		}
		var unsupported []string
		// TODO(https://github.com/istio/istio/issues/55839): implement i.Spec.SessionPersistence.
		// This will need to map into a StatefulSession filter which Istio doesn't currently support on DestinationRule
		if i.Spec.SessionPersistence != nil {
			unsupported = append(unsupported, "sessionPersistence")
		}
		if i.Spec.RetryConstraint != nil {
			// TODO: add support for interval.
			retryBudget = &networking.TrafficPolicy_RetryBudget{}
			if i.Spec.RetryConstraint.Budget.Percent != nil {
				retryBudget.Percent = &wrapperspb.DoubleValue{Value: float64(*i.Spec.RetryConstraint.Budget.Percent)}
			}
			retryBudget.MinRetryConcurrency = 10 // Gateway API default
			if i.Spec.RetryConstraint.MinRetryRate != nil {
				retryBudget.MinRetryConcurrency = uint32(*i.Spec.RetryConstraint.MinRetryRate.Count)
			}
		}
		if len(unsupported) > 0 {
			msg := fmt.Sprintf("Configuration is valid, but Istio does not support the following fields: %v", humanReadableJoin(unsupported))
			conds[string(gw.PolicyConditionAccepted)].message = msg
		}

		for idx, t := range i.Spec.TargetRefs {
			conds = maps.Clone(conds)
			refo, err := references.XLocalPolicyTargetRef(t, i.Namespace)
			if err == nil {
				switch refo.(type) {
				case *v1.Service:
				default:
					err = fmt.Errorf("unsupported reference kind: %v", t.Kind)
				}
			}
			if err != nil {
				conds[string(gw.PolicyConditionAccepted)].error = &ConfigError{
					Reason:  string(gw.PolicyReasonTargetNotFound),
					Message: "targetRefs invalid: " + err.Error(),
				}
			} else {
				// Only create an object if we can resolve the target
				res = append(res, BackendPolicy{
					Source: TypedNamespacedName{
						NamespacedName: config.NamespacedName(i),
						Kind:           kind.XBackendTrafficPolicy,
					},
					TargetIndex: idx,
					Target: TypedNamespacedName{
						NamespacedName: types.NamespacedName{
							Name:      string(t.Name),
							Namespace: i.Namespace,
						},
						Kind: kind.Service,
					},
					TLS:          nil,
					LoadBalancer: lb,
					RetryBudget:  retryBudget,
					CreationTime: i.CreationTimestamp.Time,
				})
			}

			// ancestors = append(ancestors, setAncestorStatus(t, status, i.Generation, conds))
		}
		status.Ancestors = mergeAncestors(status.Ancestors, ancestors)
		return status, res
	}, opts.WithName("BackendTrafficPolicy")...)
}

func setAncestorStatus(
	pr gw.ParentReference,
	status *gw.PolicyStatus,
	generation int64,
	conds map[string]*condition,
	controller gw.GatewayController,
) gw.PolicyAncestorStatus {
	currentAncestor := slices.FindFunc(status.Ancestors, func(ex gw.PolicyAncestorStatus) bool {
		return parentRefEqual(ex.AncestorRef, pr)
	})
	var currentConds []metav1.Condition
	if currentAncestor != nil {
		currentConds = currentAncestor.Conditions
	}
	return gw.PolicyAncestorStatus{
		AncestorRef:    pr,
		ControllerName: controller,
		Conditions:     setConditions(generation, currentConds, conds),
	}
}

func parentRefEqual(a, b gw.ParentReference) bool {
	return ptr.Equal(a.Group, b.Group) &&
		ptr.Equal(a.Kind, b.Kind) &&
		a.Name == b.Name &&
		ptr.Equal(a.Namespace, b.Namespace) &&
		ptr.Equal(a.SectionName, b.SectionName) &&
		ptr.Equal(a.Port, b.Port)
}

var outControllers = sets.New(gw.GatewayController(features.ManagedGatewayController), constants.ManagedGatewayMeshController)

// mergeAncestors merges an existing ancestor with in incoming one. We preserve order, prune stale references set by our controller,
// and add any new references from our controller.
func mergeAncestors(existing []gw.PolicyAncestorStatus, incoming []gw.PolicyAncestorStatus) []gw.PolicyAncestorStatus {
	n := 0
	for _, x := range existing {
		if !outControllers.Contains(x.ControllerName) {
			// Keep it as-is
			existing[n] = x
			n++
			continue
		}
		replacement := slices.IndexFunc(incoming, func(status gw.PolicyAncestorStatus) bool {
			return parentRefEqual(status.AncestorRef, x.AncestorRef)
		})
		if replacement != -1 {
			// We found a replacement!
			existing[n] = incoming[replacement]
			incoming = slices.Delete(incoming, replacement)
			n++
		}
		// Else, do nothing and it will be filtered
	}
	existing = existing[:n]
	// Add all remaining ones.
	existing = append(existing, incoming...)
	// There is a max of 16
	return existing[:min(len(existing), 16)]
}
