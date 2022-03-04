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

package workloadinstances

import (
	"strings"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/slices"
)

// FindInstance returns the first workload instance matching given predicate.
func FindInstance(instances []*model.WorkloadInstance, predicate func(*model.WorkloadInstance) bool) *model.WorkloadInstance {
	for _, instance := range instances {
		if predicate(instance) {
			return instance
		}
	}
	return nil
}

// InstanceNameForProxy returns a name of the workload instance that
// corresponds to a given proxy, if any.
func InstanceNameForProxy(proxy *model.Proxy) types.NamespacedName {
	parts := strings.Split(proxy.ID, ".")
	if len(parts) == 2 && proxy.ConfigNamespace == parts[1] {
		return types.NamespacedName{Name: parts[0], Namespace: parts[1]}
	}
	return types.NamespacedName{}
}

// GetInstanceForProxy returns a workload instance that
// corresponds to a given proxy, if any.
func GetInstanceForProxy(index Index, proxy *model.Proxy, proxyIP string) *model.WorkloadInstance {
	if !slices.ContainsString(proxy.IPAddresses, proxyIP) {
		return nil
	}
	instances := index.GetByIP(proxyIP) // list is ordered by namespace/name
	if len(instances) == 0 {
		return nil
	}
	if len(instances) == 1 { // dominant use case
		// NOTE: for the sake of backwards compatibility, we don't enforce
		//       instance.Namespace == proxy.ConfigNamespace
		return instances[0]
	}

	// try to find workload instance with the same name as proxy
	proxyName := InstanceNameForProxy(proxy)
	if proxyName != (types.NamespacedName{}) {
		instance := FindInstance(instances, func(wi *model.WorkloadInstance) bool {
			return wi.Name == proxyName.Name && wi.Namespace == proxyName.Namespace
		})
		if instance != nil {
			return instance
		}
	}

	// try to find workload instance in the same namespace as proxy
	instance := FindInstance(instances, func(wi *model.WorkloadInstance) bool {
		// TODO: take auto-registration group into account once it's included into workload instance
		return wi.Namespace == proxy.ConfigNamespace
	})
	if instance != nil {
		return instance
	}

	// fall back to choosing one of the workload instances

	// NOTE: for the sake of backwards compatibility, we don't enforce
	//       instance.Namespace == proxy.ConfigNamespace
	return instances[0]
}
