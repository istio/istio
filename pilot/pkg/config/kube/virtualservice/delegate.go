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
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/visibility"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/util/sets"
	"k8s.io/apimachinery/pkg/types"
)

// DelegateVirtualService is a wrapper around a VirtualService that represents a delegate
// VirtualService. It contains the VirtualService's Spec, Name, Namespace, and processed ExportTo.
type DelegateVirtualService struct {
	Spec      *networking.VirtualService
	Name      string
	Namespace string
	ExportTo  sets.Set[visibility.Instance]
}

func (dvs DelegateVirtualService) ResourceName() string {
	return types.NamespacedName{Namespace: dvs.Namespace, Name: dvs.Name}.String()
}

func (dvs DelegateVirtualService) Equals(other DelegateVirtualService) bool {
	return protoconv.Equals(dvs.Spec, other.Spec)
}

func DelegateVirtualServices(
	virtualServices krt.Collection[*networkingclient.VirtualService],
	defaultExportTo krt.Collection[ExportTo],
	opts krt.OptionsBuilder,
) krt.Collection[DelegateVirtualService] {
	return krt.NewCollection(virtualServices, func(ctx krt.HandlerContext, vs *networkingclient.VirtualService) *DelegateVirtualService {
		// this is a Root VS, we won't add these to the collection directly
		if len(vs.Spec.Hosts) > 0 {
			return nil
		}

		var exportToSet sets.Set[visibility.Instance]
		if len(vs.Spec.ExportTo) == 0 {
			// No exportTo in virtualService. Use the global default
			defaultExportTo := (*krt.FetchOne(ctx, defaultExportTo)).Set
			exportToSet = sets.NewWithLength[visibility.Instance](defaultExportTo.Len())
			for v := range defaultExportTo {
				if v == visibility.Private {
					exportToSet.Insert(visibility.Instance(vs.Namespace))
				} else {
					exportToSet.Insert(v)
				}
			}
		} else {
			exportToSet = sets.NewWithLength[visibility.Instance](len(vs.Spec.ExportTo))
			for _, e := range vs.Spec.ExportTo {
				if e == string(visibility.Private) {
					exportToSet.Insert(visibility.Instance(vs.Namespace))
				} else {
					exportToSet.Insert(visibility.Instance(e))
				}
			}
		}

		return &DelegateVirtualService{
			Spec:      &vs.Spec,
			Name:      vs.Name,
			Namespace: vs.Namespace,
			ExportTo:  exportToSet,
		}
	}, opts.WithName("DelegateVirtualServices")...)
}
