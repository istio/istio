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

package service

import (
	"fmt"

	v1 "k8s.io/api/core/v1"

	"istio.io/api/label"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/constants"
	configKube "istio.io/istio/pkg/config/kube"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/gvk"
)

// PortNameAnalyzer checks the port name of the service
type PortNameAnalyzer struct{}

var _ analysis.Analyzer = &PortNameAnalyzer{}

// Metadata implements Analyzer
func (s *PortNameAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "service.PortNameAnalyzer",
		Description: "Checks the port names associated with each service",
		Inputs: []config.GroupVersionKind{
			gvk.Service,
		},
	}
}

// Analyze implements Analyzer
func (s *PortNameAnalyzer) Analyze(c analysis.Context) {
	c.ForEach(gvk.Service, func(r *resource.Instance) bool {
		// Skip port name check for istio control plane
		if util.IsIstioControlPlane(r) {
			return true
		}

		s.analyzeService(r, c)
		return true
	})
}

func (s *PortNameAnalyzer) analyzeService(r *resource.Instance, c analysis.Context) {
	svc := r.Message.(*v1.ServiceSpec)
	// Skip gateway managed services, which are not created by users
	if v, ok := r.Metadata.Labels[label.GatewayManaged.Name]; ok &&
		v == constants.ManagedGatewayControllerLabel ||
		v == constants.ManagedGatewayMeshControllerLabel ||
		v == constants.ManagedGatewayEastWestControllerLabel {
		return
	}
	for i, port := range svc.Ports {
		instance := configKube.ConvertProtocol(port.Port, port.Name, port.Protocol, port.AppProtocol)
		if instance.IsUnsupported() || port.Name == "tcp" && svc.Type == "ExternalName" {

			m := msg.NewPortNameIsNotUnderNamingConvention(
				r, port.Name, int(port.Port), port.TargetPort.String())

			if svc.Type == "ExternalName" {
				m = msg.NewExternalNameServiceTypeInvalidPortName(r)
			}

			if line, ok := util.ErrorLine(r, fmt.Sprintf(util.PortInPorts, i)); ok {
				m.Line = line
			}
			c.Report(gvk.Service, m)
		}
	}
}
