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
package deployment

import (
	apps_v1 "k8s.io/api/apps/v1"
	core_v1 "k8s.io/api/core/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

type ServiceAssociationAnalyzer struct{}

var _ analysis.Analyzer = &ServiceAssociationAnalyzer{}

type PortMap map[int32]ProtocolMap
type ProtocolMap map[core_v1.Protocol][]ServiceSpecWithName
type ServiceNames []string
type ServiceSpecWithName struct {
	Name string
	Spec *core_v1.ServiceSpec
}

func (s *ServiceAssociationAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "deployment.MultiServiceAnalyzer",
		Inputs: collection.Names{
			metadata.K8SCoreV1Services,
			metadata.K8SAppsV1Deployments,
		},
	}
}
func (s *ServiceAssociationAnalyzer) Analyze(c analysis.Context) {
	portMap := servicePortMap(c)

	c.ForEach(metadata.K8SAppsV1Deployments, func(r *resource.Entry) bool {
		s.analyzeDeployment(r, c, portMap)
		return true
	})
}

// servicePortMap build a map of ports and protocols for each Service. e.g. m[80]["TCP"] -> svcA, svcB, svcC
func servicePortMap(c analysis.Context) PortMap {
	portMap := PortMap{}

	c.ForEach(metadata.K8SCoreV1Services, func(rs *resource.Entry) bool {
		svc := rs.Item.(*core_v1.ServiceSpec)

		for _, sPort := range svc.Ports {
			// If it is the first occurrence of this port, create a ProtocolMap
			if _, ok := portMap[sPort.Port]; !ok {
				portMap[sPort.Port] = ProtocolMap{}
			}

			// Default protocol is TCP
			protocol := sPort.Protocol
			if protocol == "" {
				protocol = core_v1.ProtocolTCP
			}

			// Appending the service information for the Port/Protocol combination
			portMap[sPort.Port][protocol] = append(portMap[sPort.Port][protocol], ServiceSpecWithName{
				rs.Metadata.Name.String(),
				svc,
			})
		}

		return true
	})

	return portMap
}

// analyzeDeployment analyzes the specific deployment given the service port map
func (s *ServiceAssociationAnalyzer) analyzeDeployment(r *resource.Entry, c analysis.Context, pm PortMap) {
	d := r.Item.(*apps_v1.Deployment)
	for _, cont := range d.Spec.Template.Spec.Containers {
		for _, portSt := range cont.Ports {
			port := portSt.ContainerPort

			// Collect Service names for each protocol
			protSvcs := map[core_v1.Protocol]ServiceNames{}
			for protocol := range pm[port] {
				for _, svc := range pm[port][protocol] {
					sSelector := k8s_labels.SelectorFromSet(svc.Spec.Selector)
					dLabels := k8s_labels.Set(d.Labels)

					if sSelector.Matches(dLabels) {
						protSvcs[protocol] = append(protSvcs[protocol], svc.Name)
					}
				}
			}

			if len(protSvcs) == 0 {
				// If there isn't any matching service, generate message. At least one service is needed.
				c.Report(metadata.K8SAppsV1Deployments, msg.NewDeploymentRequiresServiceAssociated(r, d.Name))
			} else if len(protSvcs) > 1 {
				// If there are services for more than one protocol, generate a message.
				// The services cannot use the same port number for different protocols.

				// Collecting conflicting service names
				svcNames := make(ServiceNames, 0)
				for protocol := range protSvcs {
					svcNames = append(svcNames, protSvcs[protocol]...)
				}

				// Deployment `d` at port `port` is used by the following services in different protocols: `svcNames1
				c.Report(metadata.K8SAppsV1Deployments, msg.NewDeploymentAssociatedToMultipleServices(r, d.Name, port, svcNames))
			}
		}
	}
}
