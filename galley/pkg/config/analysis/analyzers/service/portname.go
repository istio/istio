// Copyright 2019 Istio Authors
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
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
	configKube "istio.io/istio/pkg/config/kube"

	v1 "k8s.io/api/core/v1"
)

// PortNameAnalyzer checks the port name of the service
type PortNameAnalyzer struct{}

var _ analysis.Analyzer = &PortNameAnalyzer{}

// Metadata implements Analyzer
func (s *PortNameAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "service.PortNameAnalyzer",
		Description: "Checks the port names associated with each service",
		Inputs: collection.Names{
			metadata.K8SCoreV1Services,
		},
	}
}

// Analyze implements Analyzer
func (s *PortNameAnalyzer) Analyze(c analysis.Context) {
	c.ForEach(metadata.K8SCoreV1Services, func(r *resource.Instance) bool {
		s.analyzeService(r, c)
		return true
	})
}

func (s *PortNameAnalyzer) analyzeService(r *resource.Instance, c analysis.Context) {
	svc := r.Message.(*v1.ServiceSpec)
	for _, port := range svc.Ports {
		if instance := configKube.ConvertProtocol(port.Port, port.Name, port.Protocol); instance.IsUnsupported() {
			c.Report(metadata.K8SCoreV1Services, msg.NewPortNameIsNotUnderNamingConvention(
				r, port.Name, int(port.Port), port.TargetPort.String()))
		}
	}
}
