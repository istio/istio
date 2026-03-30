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

package gatewaycommon

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
		_, known := ClassInfos[obj.Spec.ControllerName]
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

func FetchGatewayClass(ctx krt.HandlerContext, gatewayClasses krt.Collection[GatewayClass], gc gatewayv1.ObjectName) *GatewayClass {
	if _, f := AgentgatewayClasses[gc]; f {
		// This function is only for fetching the gateway classes managed by the gateway controller, if the name matches the agentgateway
		// class we should return nil immediately
		return nil
	}
	class := krt.FetchOne(ctx, gatewayClasses, krt.FilterKey(string(gc)))
	if class == nil {
		if bc, f := BuiltinGatewayClasses[gc]; f {
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

func FetchAgentgatewayClass(ctx krt.HandlerContext, gatewayClasses krt.Collection[GatewayClass], gc gatewayv1.ObjectName) *GatewayClass {
	bc, f := AgentgatewayClasses[gc]
	if !f {
		// This function is only for fetching the agentgateway classes managed by the agentgateway controller, if the name doesn't match
		// we should return nil immediately
		// No gateway class found, this may be meant for another controller; should be skipped.
		return nil
	}
	class := krt.FetchOne(ctx, gatewayClasses, krt.FilterKey(string(gc)))
	if class == nil {
		// We allow some classes to exist without being in the cluster
		return &GatewayClass{
			Name:       string(gc),
			Controller: bc,
		}
	}
	return class
}
