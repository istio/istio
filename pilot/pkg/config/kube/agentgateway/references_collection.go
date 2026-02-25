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
	"k8s.io/apimachinery/pkg/types"
	gateway "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pilot/pkg/config/kube/gatewaycommon"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/kube/krt"
)

func AgwSecretAllowed(refs gatewaycommon.ReferenceGrants, ctx krt.HandlerContext, kind config.GroupVersionKind, resourceName types.NamespacedName, namespace string) bool {
	from := gatewaycommon.Reference{Kind: kind, Namespace: gateway.Namespace(namespace)}
	to := gatewaycommon.Reference{Kind: gvk.Secret, Namespace: gateway.Namespace(resourceName.Namespace)}
	pair := gatewaycommon.ReferencePair{From: from, To: to}
	grants := krt.FetchOrList(ctx, refs.Collection, krt.FilterIndex(refs.Index, pair))
	for _, g := range grants {
		if g.AllowAll || g.AllowedName == resourceName.Name {
			return true
		}
	}
	return false
}
