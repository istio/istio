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
	gatewayalpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayalpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	k8s "sigs.k8s.io/gateway-api/apis/v1beta1"
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
)

type TypedNamedspacedName struct {
	types.NamespacedName
	Kind kind.Kind
}

func (n TypedNamedspacedName) String() string {
	return n.Kind.String() + "/" + n.NamespacedName.String()
}

type BackendPolicy struct {
	Source       TypedNamedspacedName
	TargetIndex  int
	Target       TypedNamedspacedName
	SectionName  *string
	TLS          *networking.ClientTLSSettings
	LoadBalancer *networking.LoadBalancerSettings
	RetryBudget  *networking.TrafficPolicy_RetryBudget
	CreationTime time.Time
}

func (b BackendPolicy) ResourceName() string {
	return b.Source.String() + "/" + fmt.Sprint(b.TargetIndex)
}

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
	tlsPolicies krt.Collection[*gatewayalpha3.BackendTLSPolicy],
	references *ReferenceSet,
	domainSuffix string,
	c *Controller,
	services krt.Collection[*v1.Service],
	opts krt.OptionsBuilder,
) krt.Collection[*config.Config] {
	trafficPolicyStatus, backendTrafficPolicies := BackendTrafficPolicyCollection(trafficPolicies, references, opts)
	status.RegisterStatus(c.status, trafficPolicyStatus, GetStatus)

	tlsPolicyStatus, backendTLSPolicies := BackendTLSPolicyCollection(tlsPolicies, references, opts)
	status.RegisterStatus(c.status, tlsPolicyStatus, GetStatus)

	// We need to merge these by hostname into a single DR
	allPolicies := krt.JoinCollection([]krt.Collection[BackendPolicy]{backendTrafficPolicies, backendTLSPolicies})
	byTarget := krt.NewIndex(allPolicies, "target", func(o BackendPolicy) []TypedNamedspacedName {
		return []TypedNamedspacedName{o.Target}
	})
	indexOpts := append(opts.WithName("BackendPolicyByTarget"), krt.WithIndexCollectionFromString(func(s string) TypedNamedspacedName {
		parts := strings.Split(s, "/")
		if len(parts) != 3 {
			panic("invalid TypedNamedspacedName: " + s)
		}
		return TypedNamedspacedName{
			NamespacedName: types.NamespacedName{
				Namespace: parts[1],
				Name:      parts[2],
			},
			Kind: kind.FromString(parts[0]),
		}
	}))
	merged := krt.NewCollection(
		byTarget.AsCollection(indexOpts...),
		func(ctx krt.HandlerContext, i krt.IndexObject[TypedNamedspacedName, BackendPolicy]) **config.Config {
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
	tlsPolicies krt.Collection[*gatewayalpha3.BackendTLSPolicy],
	references *ReferenceSet,
	opts krt.OptionsBuilder,
) (krt.StatusCollection[*gatewayalpha3.BackendTLSPolicy, gatewayalpha2.PolicyStatus], krt.Collection[BackendPolicy]) {
	return krt.NewStatusManyCollection(tlsPolicies, func(ctx krt.HandlerContext, i *gatewayalpha3.BackendTLSPolicy) (
		*gatewayalpha2.PolicyStatus,
		[]BackendPolicy,
	) {
		status := i.Status.DeepCopy()
		res := make([]BackendPolicy, 0, len(i.Spec.TargetRefs))
		ancestors := make([]gatewayalpha2.PolicyAncestorStatus, 0, len(i.Spec.TargetRefs))

		tls := &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE}
		s := i.Spec

		conds := map[string]*condition{
			string(gatewayalpha2.PolicyConditionAccepted): {
				reason:  string(gatewayalpha2.PolicyReasonAccepted),
				message: "Configuration is valid",
			},
		}
		tls.Sni = string(s.Validation.Hostname)
		tls.SubjectAltNames = slices.MapFilter(s.Validation.SubjectAltNames, func(e gatewayalpha3.SubjectAltName) *string {
			switch e.Type {
			case gatewayalpha3.HostnameSubjectAltNameType:
				return ptr.Of(string(e.Hostname))
			case gatewayalpha3.URISubjectAltNameType:
				return ptr.Of(string(e.URI))
			}
			return nil
		})
		tls.CredentialName = getBackendTLSCredentialName(s.Validation, i.Namespace, conds, references)
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
				conds[string(gatewayalpha2.PolicyConditionAccepted)].error = &ConfigError{
					Reason:  string(gatewayalpha2.PolicyReasonTargetNotFound),
					Message: "targetRefs invalid: " + err.Error(),
				}
			} else {
				// Only create an object if we can resolve the target
				res = append(res, BackendPolicy{
					Source: TypedNamedspacedName{
						NamespacedName: config.NamespacedName(i),
						Kind:           kind.BackendTLSPolicy,
					},
					TargetIndex: idx,
					Target: TypedNamedspacedName{
						NamespacedName: types.NamespacedName{
							Name:      string(t.Name),
							Namespace: i.Namespace,
						},
						Kind: gvk.MustToKind(schematypes.GvkFromObject(refo.(controllers.Object))),
					},
					SectionName:  sectionName,
					TLS:          tls,
					CreationTime: i.CreationTimestamp.Time,
				})
			}
			ancestors = append(ancestors, setAncestorStatus(t.LocalPolicyTargetReference, status, i.Generation, conds))
		}
		status.Ancestors = mergeAncestors(status.Ancestors, ancestors)
		return status, res
	}, opts.WithName("BackendTLSPolicy")...)
}

func getBackendTLSCredentialName(
	validation gatewayalpha3.BackendTLSPolicyValidation,
	policyNamespace string,
	conds map[string]*condition,
	references *ReferenceSet,
) string {
	if wk := validation.WellKnownCACertificates; wk != nil {
		switch *wk {
		case gatewayalpha3.WellKnownCACertificatesSystem:
			// Already our default, no action needed
		default:
			conds[string(gatewayalpha2.PolicyConditionAccepted)].error = &ConfigError{
				Reason:  string(gatewayalpha2.PolicyReasonInvalid),
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
		conds[string(gatewayalpha2.PolicyConditionAccepted)].message += "; warning: only the first caCertificateRefs will be used"
	}
	refo, err := references.LocalPolicyRef(ref, policyNamespace)
	if err == nil {
		switch to := refo.(type) {
		case *v1.ConfigMap:
			if _, rerr := kubesecrets.ExtractRootFromString(to.Data); rerr != nil {
				err = rerr
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
		}
	}
	if err != nil {
		conds[string(gatewayalpha2.PolicyConditionAccepted)].error = &ConfigError{
			Reason:  string(gatewayalpha2.PolicyReasonInvalid),
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
		ancestors := make([]gatewayalpha2.PolicyAncestorStatus, 0, len(i.Spec.TargetRefs))

		lb := &networking.LoadBalancerSettings{}
		var retryBudget *networking.TrafficPolicy_RetryBudget

		conds := map[string]*condition{
			string(gatewayalpha2.PolicyConditionAccepted): {
				reason:  string(gatewayalpha2.PolicyReasonAccepted),
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
			conds[string(gatewayalpha2.PolicyConditionAccepted)].message = msg
		}

		for idx, t := range i.Spec.TargetRefs {
			conds = maps.Clone(conds)
			refo, err := references.LocalPolicyTargetRef(t, i.Namespace)
			if err == nil {
				switch refo.(type) {
				case *v1.Service:
				default:
					err = fmt.Errorf("unsupported reference kind: %v", t.Kind)
				}
			}
			if err != nil {
				conds[string(gatewayalpha2.PolicyConditionAccepted)].error = &ConfigError{
					Reason:  string(gatewayalpha2.PolicyReasonTargetNotFound),
					Message: "targetRefs invalid: " + err.Error(),
				}
			} else {
				// Only create an object if we can resolve the target
				res = append(res, BackendPolicy{
					Source: TypedNamedspacedName{
						NamespacedName: config.NamespacedName(i),
						Kind:           kind.XBackendTrafficPolicy,
					},
					TargetIndex: idx,
					Target: TypedNamedspacedName{
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
			ancestors = append(ancestors, setAncestorStatus(t, status, i.Generation, conds))
		}
		status.Ancestors = mergeAncestors(status.Ancestors, ancestors)
		return status, res
	}, opts.WithName("BackendTrafficPolicy")...)
}

func setAncestorStatus(
	t gatewayalpha2.LocalPolicyTargetReference,
	status *gatewayalpha2.PolicyStatus,
	generation int64,
	conds map[string]*condition,
) gatewayalpha2.PolicyAncestorStatus {
	pr := gatewayalpha2.ParentReference{
		Group: &t.Group,
		Kind:  &t.Kind,
		Name:  t.Name,
	}
	currentAncestor := slices.FindFunc(status.Ancestors, func(ex gatewayalpha2.PolicyAncestorStatus) bool {
		return parentRefEqual(ex.AncestorRef, pr)
	})
	var currentConds []metav1.Condition
	if currentAncestor != nil {
		currentConds = currentAncestor.Conditions
	}
	return gatewayalpha2.PolicyAncestorStatus{
		AncestorRef:    pr,
		ControllerName: k8s.GatewayController(features.ManagedGatewayController),
		Conditions:     setConditions(generation, currentConds, conds),
	}
}

func parentRefEqual(a, b gatewayalpha2.ParentReference) bool {
	return ptr.Equal(a.Group, b.Group) &&
		ptr.Equal(a.Kind, b.Kind) &&
		a.Name == b.Name &&
		ptr.Equal(a.Namespace, b.Namespace) &&
		ptr.Equal(a.SectionName, b.SectionName) &&
		ptr.Equal(a.Port, b.Port)
}

// mergeAncestors merges an existing ancestor with in incoming one. We preserve order, prune stale references set by our controller,
// and add any new references from our controller.
func mergeAncestors(existing []gatewayalpha2.PolicyAncestorStatus, incoming []gatewayalpha2.PolicyAncestorStatus) []gatewayalpha2.PolicyAncestorStatus {
	ourController := k8s.GatewayController(features.ManagedGatewayController)
	n := 0
	for _, x := range existing {
		if x.ControllerName != ourController {
			// Keep it as-is
			existing[n] = x
			n++
			continue
		}
		replacement := slices.IndexFunc(incoming, func(status gatewayalpha2.PolicyAncestorStatus) bool {
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
	return existing
}
