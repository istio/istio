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
	"fmt"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/util/sets"
)

type RBACGenerator struct {
	s *DiscoveryServer
}

var (
	_ model.XdsResourceGenerator      = &RBACGenerator{}
	_ model.XdsDeltaResourceGenerator = &RBACGenerator{}
)

// GenerateDeltas computes RBAC resources. A client can subscribe with a wildcard subscription and get all
// resources (with delta updates).
func (e RBACGenerator) GenerateDeltas(
	proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	configsUpdated := model.ConfigNamespacedNameOfKind(req.ConfigsUpdated, kind.AuthorizationPolicy)
	isReq := req.IsRequest()
	updatedPolicies := sets.New[string]()

	for updatedPolicy := range configsUpdated {
		if w.Wildcard {
			updatedPolicies.Insert(updatedPolicy.Name + "/" + updatedPolicy.Namespace)
		}
	}

	// Specific requested resource: always include
	for policy := range req.Delta.Subscribed {
		updatedPolicies.Insert(policy)
	}

	full := (isReq && w.Wildcard) || (!isReq && req.Full && len(req.ConfigsUpdated) == 0)

	// Nothing to do
	if len(updatedPolicies) == 0 && !full {
		if isReq {
			// We need to respond for requests, even if we have nothing to respond with
			return make(model.Resources, 0), nil, model.XdsLogDetails{}, false, nil
		}
		// For NOP pushes, no need
		return nil, nil, model.XdsLogDetails{}, false, nil
	}

	resources := make(model.Resources, 0)
	have := sets.New[string]()

	// Go through the authorization policies and add an RBAC resource for each
	// one that is also in the updated list
	rootNS := req.Push.AuthzPolicies.RootNamespace
	for ns, policies := range req.Push.AuthzPolicies.NamespaceToPolicies {
		for _, policy := range policies {
			rbacName := policy.Name + "/" + ns
			have.Insert(rbacName)

			if !updatedPolicies.Contains(rbacName) {
				continue
			}

			resources = append(resources, &discovery.Resource{
				Name:     rbacName,
				Resource: protoconv.MessageToAny(authorizationPolicyToRBAC(policy, rootNS)),
			})
		}
	}

	// Updated resources which aren't in the existing resources are removed
	removes := updatedPolicies.Difference(have)
	removed := sets.SortedList(removes)

	return resources, removed, model.XdsLogDetails{}, true, nil
}

func (e RBACGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	return nil, model.XdsLogDetails{}, fmt.Errorf("RBACDS is only available over Delta XDS")
}
