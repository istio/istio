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
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/workloadapi"
	"k8s.io/apimachinery/pkg/types"

	inferencev1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
)

type Address struct {
	Workload *model.WorkloadInfo
	Service  *model.ServiceInfo
}

func (i Address) ResourceName() string {
	if i.Workload != nil {
		return i.Workload.ResourceName()
	}
	return i.Service.ResourceName()
}

func (i Address) Equals(other Address) bool {
	if (i.Workload != nil) != (other.Workload != nil) {
		return false
	}
	if i.Workload != nil {
		return i.Workload.Equals(*other.Workload)
	}
	return i.Service.Equals(*other.Service)
}

func (i Address) IntoProto() *workloadapi.Address {
	if i.Workload != nil {
		return i.Workload.AsAddress.Address
	}
	return i.Service.AsAddress.Address
}

// InferenceHostname produces FQDN for a k8s service
func InferenceHostname(name, namespace, domainSuffix string) host.Name {
	return host.Name(name + "." + namespace + "." + "inference" + "." + domainSuffix) // Format: "%s.%s.svc.%s"
}

func precomputeServicePtr(w *model.ServiceInfo) *model.ServiceInfo {
	return ptr.Of(precomputeService(*w))
}

func precomputeService(w model.ServiceInfo) model.ServiceInfo {
	addr := serviceToAddress(w.Service)
	w.MarshaledAddress = protoconv.MessageToAny(addr)
	w.AsAddress = model.AddressInfo{
		Address:   addr,
		Marshaled: w.MarshaledAddress,
	}
	return w
}

func serviceToAddress(s *workloadapi.Service) *workloadapi.Address {
	return &workloadapi.Address{
		Type: &workloadapi.Address_Service{
			Service: s,
		},
	}
}

func inferencePoolBuilder(domainSuffix string) krt.TransformationSingle[*inferencev1.InferencePool, model.ServiceInfo] {
	return func(ctx krt.HandlerContext, s *inferencev1.InferencePool) *model.ServiceInfo {
		portNames := map[int32]model.ServicePortName{}
		ports := []*workloadapi.Port{{
			ServicePort: uint32(s.Spec.TargetPorts[0].Number), //nolint:gosec // G115: InferencePool TargetPort is int32 with validation 1-65535, always safe
			TargetPort:  uint32(s.Spec.TargetPorts[0].Number), //nolint:gosec // G115: InferencePool TargetPort is int32 with validation 1-65535, always safe
			AppProtocol: workloadapi.AppProtocol_HTTP11,
		}}

		// TODO this is only checking one controller - we may be missing service vips for instances in another cluster
		svc := &workloadapi.Service{
			Name:      s.Name,
			Namespace: s.Namespace,
			Hostname:  string(InferenceHostname(s.Name, s.Namespace, domainSuffix)),
			Ports:     ports,
		}

		selector := make(map[string]string, len(s.Spec.Selector.MatchLabels))
		for k, v := range s.Spec.Selector.MatchLabels {
			selector[string(k)] = string(v)
		}
		return precomputeServicePtr(&model.ServiceInfo{
			Service:       svc,
			PortNames:     portNames,
			LabelSelector: model.NewSelector(selector),
			Source: model.TypedObject{
				NamespacedName: types.NamespacedName{
					Namespace: s.Namespace,
					Name:      s.Name,
				},
				Kind: kind.InferencePool,
			},
		})
	}
}
