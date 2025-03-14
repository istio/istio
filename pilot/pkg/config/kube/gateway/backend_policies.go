package gateway

import (
	"cmp"
	"fmt"
	"strings"
	"time"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1"
	gatewayalpha "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayalpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
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
	TLS          *networking.ClientTLSSettings
	LoadBalancer *networking.LoadBalancerSettings
	CreationTime time.Time
}

func (b BackendPolicy) ResourceName() string {
	return b.Source.String() + "/" + fmt.Sprint(b.TargetIndex)
}

func (b BackendPolicy) Equals(other BackendPolicy) bool {
	return b.Source == other.Source &&
		protoconv.Equals(b.TLS, other.TLS) &&
		protoconv.Equals(b.LoadBalancer, other.LoadBalancer)
}

func DestinationRuleCollection(
	lbPolicies krt.Collection[*gatewayalpha.BackendLBPolicy],
	tlsPolicies krt.Collection[*gatewayalpha3.BackendTLSPolicy],
	domainSuffix string,
	opts krt.OptionsBuilder,
) krt.Collection[*config.Config] {
	backendLbPolicies := krt.NewManyCollection(lbPolicies, func(ctx krt.HandlerContext, i *gatewayalpha.BackendLBPolicy) []BackendPolicy {
		res := make([]BackendPolicy, 0, len(i.Spec.TargetRefs))

		lb := &networking.LoadBalancerSettings{}
		if i.Spec.SessionPersistence != nil {
			sp := *i.Spec.SessionPersistence
			switch ptr.OrDefault(sp.Type, gateway.CookieBasedSessionPersistence) {
			case gateway.CookieBasedSessionPersistence:
			case gateway.HeaderBasedSessionPersistence:
			}
		}
		for idx, t := range i.Spec.TargetRefs {
			if !(t.Group == "" && t.Kind == "Service") {
				continue
			}

			res = append(res, BackendPolicy{
				Source: TypedNamedspacedName{
					NamespacedName: config.NamespacedName(i),
					Kind:           kind.BackendLBPolicy,
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
				CreationTime: i.CreationTimestamp.Time,
			})
		}
		return res
	}, opts.WithName("BackendLBPolicy")...)
	backendTLSPolicies := krt.NewManyCollection(tlsPolicies, func(ctx krt.HandlerContext, i *gatewayalpha3.BackendTLSPolicy) []BackendPolicy {
		res := make([]BackendPolicy, 0, len(i.Spec.TargetRefs))

		tls := &networking.ClientTLSSettings{Mode: networking.ClientTLSSettings_SIMPLE}
		s := i.Spec

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
		if wk := s.Validation.WellKnownCACertificates; wk != nil {
			switch *wk {
			case gatewayalpha3.WellKnownCACertificatesSystem:
				// Already our default, no action needed
			}
		} else if len(s.Validation.CACertificateRefs) > 0 {
			// Spec should require but double check
			// We only support 1
			ref := s.Validation.CACertificateRefs[0]
			if string(ref.Kind) == gvk.ConfigMap.Kind && ref.Group == "" {
				// TODO: implement the control plane side
				tls.CredentialName = credentials.KubernetesConfigMapTypeURI  + i.Namespace + "/" + string(ref.Name)
			} else if string(ref.Kind) == gvk.Secret.Kind && ref.Group == "" {
				tls.CredentialName = string(ref.Name)
			}
		}
		for idx, t := range i.Spec.TargetRefs {
			if !(t.Group == "" && t.Kind == "Service") {
				continue
			}

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
					Kind: kind.Service,
				},
				TLS:          tls,
				CreationTime: i.CreationTimestamp.Time,
			})
		}
		return res
	}, opts.WithName("BackendTLSPolicy")...)
	allPolicies := krt.JoinCollection([]krt.Collection[BackendPolicy]{backendLbPolicies, backendTLSPolicies})
	byTarget := krt.NewIndex(allPolicies, func(o BackendPolicy) []TypedNamedspacedName {
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
			// Sort so we can pick the oldest, which will win
			pols := slices.SortFunc(i.Objects, func(a, b BackendPolicy) int {
				if r := a.CreationTime.Compare(b.CreationTime); r != 0 {
					return r
				}
				if r := cmp.Compare(a.Source.Namespace, b.Source.Namespace); r != 0 {
					return r
				}
				return cmp.Compare(a.Source.Name, b.Source.Name)
			})
			_ = pols
			tlsSet := false
			lbSet := false
			spec := &networking.DestinationRule{
				Host:             fmt.Sprintf("%s.%s.svc.%v", svc.Name, svc.Namespace, domainSuffix),
				TrafficPolicy:    &networking.TrafficPolicy{},
				Subsets:          nil,
				ExportTo:         nil,
				WorkloadSelector: nil,
			}
			for _, pol := range pols {
				if pol.TLS != nil {
					if tlsSet {
						// We only allow 1. TODO: status
						continue
					}
					tlsSet = true
					spec.TrafficPolicy.Tls = pol.TLS
				}
				if pol.LoadBalancer != nil {
					if lbSet {
						// We only allow 1. TODO: status
						continue
					}
					lbSet = true
					spec.TrafficPolicy.LoadBalancer = pol.LoadBalancer
				}
			}
			cfg := &config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.DestinationRule,
					Name:             fmt.Sprintf("%s-%s", svc.Name, constants.KubernetesGatewayName),
					Namespace:        svc.Namespace,
				},
				Spec: spec,
			}
			return &cfg
		}, opts.WithName("BackendPolicyMerged")...)
	return merged
}
