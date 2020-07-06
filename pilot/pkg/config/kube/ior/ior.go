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

package ior

import (
	"strings"

	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/pkg/log"

	"k8s.io/client-go/kubernetes"
)

var iorLog = log.RegisterScope("ior", "IOR logging", 0)

// Register configures IOR component to respond to Gateway creations and removals
func Register(client kubernetes.Interface, store model.ConfigStoreCache, pilotNamespace string) {
	iorLog.Info("Registering IOR component")

	if !isRouteSupported(client) {
		iorLog.Error("OpenShift routes are not supported in this cluster. IOR is not enabled.")
		return
	}

	r, err := newRoute(client, store, pilotNamespace)
	if err != nil {
		iorLog.Errora(err)
		return
	}

	kind := collections.IstioNetworkingV1Alpha3Gateways.Resource().GroupVersionKind()
	store.RegisterEventHandler(kind, func(_, curr model.Config, event model.Event) {
		_, ok := curr.Spec.(*networking.Gateway)
		if !ok {
			iorLog.Errorf("could not decode object as Gateway. Object = %v", curr)
			return
		}

		iorLog.Debugf("Event %v arrived. Object: %v", event, curr)
		if err := r.syncGatewaysAndRoutes(); err != nil {
			iorLog.Errora(err)
		}

	})
}

func isRouteSupported(client kubernetes.Interface) bool {
	_, s, _ := client.Discovery().ServerGroupsAndResources()
	// This may fail if any api service is down, but the result will still be populated, so we skip the error
	for _, res := range s {
		for _, api := range res.APIResources {
			if api.Kind == "Route" && strings.HasPrefix(res.GroupVersion, "route.openshift.io/") {
				return true
			}
		}
	}
	return false
}
