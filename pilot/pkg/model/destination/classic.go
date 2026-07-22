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

package destination

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
)

// ClassicProjection is the compatibility view used by existing CDS/EDS and
// aggregate service registry consumers. It does not imply a frontend.
type ClassicProjection struct {
	Service   *model.Service
	Endpoints []*model.IstioEndpoint
}

func ProjectClassic(resolved ResolvedDestination, registry provider.ID) ClassicProjection {
	d := resolved.Definition
	p := &model.Port{Name: resolved.Binding.Port.Name, Port: resolved.Binding.Port.Number, Protocol: resolved.Binding.Port.Protocol}
	service := &model.Service{
		Attributes: model.ServiceAttributes{
			ServiceRegistry: registry,
			Name:            resolved.Binding.RuntimeName.String(), Namespace: d.Namespace,
			K8sAttributes: model.K8sAttributes{ObjectName: d.ID.Source.Name},
		},
		Ports: model.PortList{p}, CreationTime: resolved.Binding.CreationTime,
		Hostname: resolved.Binding.RuntimeName, DefaultAddress: constants.UnspecifiedIP,
		Resolution: resolutionFor(resolved.Binding.Endpoints.Kind), MeshExternal: true,
	}
	if d.Metadata.Semantics == InferencePoolSemantics {
		service.Attributes.Labels = labels.Instance{
			constants.InternalServiceSemantics: constants.ServiceSemanticsInferencePool,
		}
	}
	return ClassicProjection{Service: service, Endpoints: append([]*model.IstioEndpoint(nil), resolved.Endpoints...)}
}

func resolutionFor(source EndpointSourceKind) model.Resolution {
	switch source {
	case DNS:
		return model.DNSLB
	case DynamicDNS:
		return model.DynamicDNS
	default:
		return model.ClientSideLB
	}
}
