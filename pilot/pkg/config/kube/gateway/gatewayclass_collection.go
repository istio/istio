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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/istio/pkg/kube/krt"
)

type GatewayClass struct {
	Name       string
	Controller gatewayv1.GatewayController
}

func (g GatewayClass) ResourceName() string {
	return g.Name
}

func GatewayClassesCollection(
	gatewayClasses krt.Collection[*gatewayv1.GatewayClass],
	opts krt.OptionsBuilder,
) (
	krt.StatusCollection[*gatewayv1.GatewayClass, gatewayv1.GatewayClassStatus],
	krt.Collection[GatewayClass],
) {
	return krt.NewStatusCollection(gatewayClasses, func(ctx krt.HandlerContext, obj *gatewayv1.GatewayClass) (*gatewayv1.GatewayClassStatus, *GatewayClass) {
		_, known := classInfos[obj.Spec.ControllerName]
		if !known {
			return nil, nil
		}
		status := obj.Status.DeepCopy()
		status = GetClassStatus(status, obj.Generation)
		return status, &GatewayClass{
			Name:       obj.Name,
			Controller: obj.Spec.ControllerName,
		}
	}, opts.WithName("GatewayClasses")...)
}

func fetchClass(ctx krt.HandlerContext, gatewayClasses krt.Collection[GatewayClass], gc gatewayv1.ObjectName) *GatewayClass {
	class := krt.FetchOne(ctx, gatewayClasses, krt.FilterKey(string(gc)))
	if class == nil {
		if bc, f := builtinClasses[gc]; f {
			// We allow some classes to exist without being in the cluster
			return &GatewayClass{
				Name:       string(gc),
				Controller: bc,
			}
		}
		// No gateway class found, this may be meant for another controller; should be skipped.
		return nil
	}
	return class
}
