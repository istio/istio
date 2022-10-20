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

package xds

import (
	"strconv"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/security/authn/factory"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/schema/kind"
	workloadapi "istio.io/istio/pkg/workloadapi"
)

type WorkloadGenerator struct {
	s *DiscoveryServer
}

var (
	_ model.XdsResourceGenerator      = &WorkloadGenerator{}
	_ model.XdsDeltaResourceGenerator = &WorkloadGenerator{}
)

func (e WorkloadGenerator) GenerateDeltas(
	proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	// TODO: wdsNeedsPush
	resources := make(model.Resources, 0)
	// Workload consists of two dependencies: Pod and Service
	// For now, we optimize Pod updates but not Service
	// As long as there are no Service updates, we will do a delta push
	pods := model.ConfigsOfKind(req.ConfigsUpdated, kind.Pod)
	svcs := model.ConfigsOfKind(req.ConfigsUpdated, kind.Service)
	if len(req.ConfigsUpdated) == 0 {
		pods = nil
	}
	if len(svcs) > 0 {
		pods = nil
	}
	usedDelta := pods != nil
	wls, removed := e.s.Env.ServiceDiscovery.PodInformation(pods)
	for _, wl := range wls {
		rbac := reduceAuthz(req.Push.AuthzPolicies.ListAuthorizationPolicies(wl.Namespace, wl.Labels))
		// TODO allow per-port
		if factory.NewPolicyApplier(req.Push, wl.Namespace, wl.Labels).GetMutualTLSModeForPort(0) == model.MTLSStrict {
			if rbac == nil {
				rbac = &workloadapi.Authorization{}
			}
			rbac.EnforceTLS = true
			wl.Enforce = true
		}
		wl.Rbac = rbac
		resources = append(resources, &discovery.Resource{
			Name:     wl.ResourceName(),
			Resource: protoconv.MessageToAny(wl),
		})
	}
	return resources, removed, model.XdsLogDetails{}, usedDelta, nil
}

func (e WorkloadGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	rr := &model.PushRequest{
		Full:           req.Full,
		ConfigsUpdated: nil,
		Push:           req.Push,
		Start:          req.Start,
		Reason:         req.Reason,
		Delta:          req.Delta,
	}
	res, deleted, log, usedDelta, err := e.GenerateDeltas(proxy, rr, w)
	if len(deleted) > 0 || usedDelta {
		panic("invalid delta usage")
	}
	return res, log, err
}

// Hacky solution to create a condition that """never""" occurs.
// DO NOT SHIP TO PROD
var conditionNever = &workloadapi.AuthCondition{
	Port: 99999,
}

func reduceAuthz(policies model.AuthorizationPoliciesResult) *workloadapi.Authorization {
	if len(policies.Deny) == 0 && len(policies.Allow) == 0 {
		return nil
	}
	port := func(s string) uint32 {
		i, _ := strconv.Atoi(s)
		return uint32(i)
	}
	policy := &workloadapi.Authorization{}
	for _, p := range policies.Allow {
		for _, r := range p.Spec.Rules {
			rules := []*workloadapi.AuthRule{}
			conditions := []*workloadapi.AuthCondition{}
			for _, from := range r.From {
				for _, s := range from.Source.Namespaces {
					rules = append(rules, &workloadapi.AuthRule{
						Namespace: s,
					})
				}
				for _, s := range from.Source.NotNamespaces {
					rules = append(rules, &workloadapi.AuthRule{
						Namespace: s,
						Invert:    true,
					})
				}
				for _, s := range from.Source.Principals {
					rules = append(rules, &workloadapi.AuthRule{
						Identity: s,
					})
				}
				for _, s := range from.Source.NotPrincipals {
					rules = append(rules, &workloadapi.AuthRule{
						Identity: s,
						Invert:   true,
					})
				}
			}
			for _, to := range r.To {
				if hasHTTP(to) {
					// if there are HTTP rules, we can never meet them, since this is L4 only. So replace with
					// never matching rule
					conditions = append(conditions, conditionNever)
					continue
				}
				for _, o := range to.Operation.Ports {
					conditions = append(conditions, &workloadapi.AuthCondition{
						Port: port(o),
					})
				}
				for _, o := range to.Operation.NotPorts {
					conditions = append(conditions, &workloadapi.AuthCondition{
						Port:   port(o),
						Invert: true,
					})
				}
			}
			policy.Allow = append(policy.Allow, &workloadapi.Policy{
				Rule: rules,
				When: conditions,
			})
		}
	}
	for _, p := range policies.Deny {
		for _, r := range p.Spec.Rules {
			rules := []*workloadapi.AuthRule{}
			conditions := []*workloadapi.AuthCondition{}
			for _, from := range r.From {
				for _, s := range from.Source.Namespaces {
					rules = append(rules, &workloadapi.AuthRule{
						Namespace: s,
					})
				}
				for _, s := range from.Source.NotNamespaces {
					rules = append(rules, &workloadapi.AuthRule{
						Namespace: s,
						Invert:    true,
					})
				}
				for _, s := range from.Source.Principals {
					rules = append(rules, &workloadapi.AuthRule{
						Identity: s,
					})
				}
				for _, s := range from.Source.NotPrincipals {
					rules = append(rules, &workloadapi.AuthRule{
						Identity: s,
						Invert:   true,
					})
				}
			}
			for _, to := range r.To {
				for _, o := range to.Operation.Ports {
					conditions = append(conditions, &workloadapi.AuthCondition{
						Port: port(o),
					})
				}
				for _, o := range to.Operation.NotPorts {
					conditions = append(conditions, &workloadapi.AuthCondition{
						Port:   port(o),
						Invert: true,
					})
				}
			}
			policy.Deny = append(policy.Deny, &workloadapi.Policy{
				Rule: rules,
				When: conditions,
			})
		}
	}
	return policy
}

func hasHTTP(to *v1beta1.Rule_To) bool {
	if len(to.Operation.Hosts) > 0 {
		return true
	}
	if len(to.Operation.NotHosts) > 0 {
		return true
	}
	if len(to.Operation.Methods) > 0 {
		return true
	}
	if len(to.Operation.NotMethods) > 0 {
		return true
	}
	if len(to.Operation.Paths) > 0 {
		return true
	}
	if len(to.Operation.NotPaths) > 0 {
		return true
	}
	return false
}
