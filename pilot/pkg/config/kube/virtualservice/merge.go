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

package virtualservice

import (
	networking "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/protomarshal"
	"k8s.io/apimachinery/pkg/types"
)

func MergeVirtualServices(
	virtualServices krt.Collection[*networkingclient.VirtualService],
	delegateVirtualServices krt.Collection[DelegateVirtualService],
	opts krt.OptionsBuilder,
) krt.Collection[config.Config] {
	return krt.NewCollection(virtualServices, func(ctx krt.HandlerContext, vs *networkingclient.VirtualService) *config.Config {
		// this is a Delegate VS, we won't add these to the collection directly
		if len(vs.Spec.Hosts) == 0 {
			return nil
		}

		// if this VS does not reference any delegate, we won't add it to the collection
		if !isRootVs(&vs.Spec) {
			return nil
		}

		root := vs.DeepCopy()

		mergedRoutes := []*networking.HTTPRoute{}
		for _, http := range root.Spec.Http {
			if delegate := http.Delegate; delegate != nil {
				delegateNamespace := delegate.Namespace
				if delegateNamespace == "" {
					delegateNamespace = root.Namespace
				}

				key := types.NamespacedName{Namespace: delegateNamespace, Name: delegate.Name}
				delegateVs := krt.FetchOne(ctx, delegateVirtualServices, krt.FilterObjectName(key))
				if delegateVs == nil {
					log.Warnf("delegate virtual service %s/%s of %s/%s not found",
						delegateNamespace, delegate.Name, root.Namespace, root.Name)
					// delegate not found, ignore only the current HTTP route
					continue
				}

				// make sure that the delegate is visible to root virtual service's namespace
				if !delegateVs.ExportTo.Contains(visibility.Public) && !delegateVs.ExportTo.Contains(visibility.Instance(root.Namespace)) {
					log.Warnf("delegate virtual service %s/%s of %s/%s is not exported to %s",
						delegateNamespace, delegate.Name, root.Namespace, root.Name, root.Namespace)
					continue
				}

				// DeepCopy to prevent mutate the original delegate, it can conflict
				// when multiple routes delegate to one single VS.
				copiedDelegate := delegateVs.Spec.DeepCopy()
				merged := model.MergeHTTPRoutes(http, copiedDelegate.Http)
				mergedRoutes = append(mergedRoutes, merged...)
			} else {
				mergedRoutes = append(mergedRoutes, http)
			}
		}

		root.Spec.Http = mergedRoutes
		if log.DebugEnabled() {
			vsString, _ := protomarshal.ToJSONWithIndent(&root.Spec, "   ")
			log.Debugf("merged virtualService: %s", vsString)
		}

		config := model.ResolveVirtualServiceShortnames(vsToConfig(root))
		return &config
	}, opts.WithName("MergedVirtualServices")...)
}

func isRootVs(vs *networking.VirtualService) bool {
	for _, route := range vs.Http {
		// it is root vs with delegate
		if route.Delegate != nil {
			return true
		}
	}
	return false
}

func UseGatewaySemantics(vs *networkingclient.VirtualService) bool {
	return vs.Annotations[constants.InternalRouteSemantics] == constants.RouteSemanticsGateway
}

func vsToConfig(vs *networkingclient.VirtualService) config.Config {
	return config.Config{
		Meta: config.Meta{
			GroupVersionKind:  gvk.VirtualService,
			Name:              vs.Name,
			Namespace:         vs.Namespace,
			Labels:            vs.Labels,
			Annotations:       vs.Annotations,
			ResourceVersion:   vs.ResourceVersion,
			CreationTimestamp: vs.CreationTimestamp.Time,
			OwnerReferences:   vs.OwnerReferences,
			UID:               string(vs.UID),
			Generation:        vs.Generation,
		},
		Spec:   &vs.Spec,
		Status: &vs.Status,
	}
}
