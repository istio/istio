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
	gateway "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	creds "istio.io/istio/pilot/pkg/model/credentials"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/kube/krt"
)

func SecretAllowed(refs gatewaycommon.ReferenceGrants, ctx krt.HandlerContext, kind config.GroupVersionKind, resourceName string, namespace string) bool {
	p, err := creds.ParseResourceName(resourceName, "", "", "")
	if err != nil {
		log.Warnf("failed to parse resource name %q: %v", resourceName, err)
		return false
	}
	resourceKind := config.GroupVersionKind{Kind: p.ResourceKind.String()}
	resourceSchema, resourceSchemaFound := collections.All.FindByGroupKind(resourceKind)
	if resourceSchemaFound {
		resourceKind = resourceSchema.GroupVersionKind()
	}
	from := gatewaycommon.Reference{Kind: kind, Namespace: gateway.Namespace(namespace)}
	to := gatewaycommon.Reference{Kind: resourceKind, Namespace: gateway.Namespace(p.Namespace)}
	pair := gatewaycommon.ReferencePair{From: from, To: to}
	grants := krt.FetchOrList(ctx, refs.Collection, krt.FilterIndex(refs.Index, pair))
	for _, g := range grants {
		if g.AllowAll || g.AllowedName == p.Name {
			return true
		}
	}
	return false
}
