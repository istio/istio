// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package deployment

import (
	"fmt"
	"strconv"

	apps_v1 "k8s.io/api/apps/v1"
	core_v1 "k8s.io/api/core/v1"
	k8s_labels "k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/util"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

type ServiceAssociationAnalyzer struct{}

var _ analysis.Analyzer = &ServiceAssociationAnalyzer{}

type (
	PortMap             map[int32]ProtocolMap
	ProtocolMap         map[core_v1.Protocol]ServiceNames
	ServiceNames        []string
	ServiceSpecWithName struct {
		Name string
		Spec *core_v1.ServiceSpec
	}
)

// targetPort port serviceName
type targetPortMap map[string]map[int32]string

func (s *ServiceAssociationAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "deployment.MultiServiceAnalyzer",
		Description: "Checks association between services and pods",
		Inputs: collection.Names{
			collections.K8SCoreV1Services.Name(),
			collections.K8SAppsV1Deployments.Name(),
			collections.K8SCoreV1Namespaces.Name(),
		},
	}
}

func (s *ServiceAssociationAnalyzer) Analyze(c analysis.Context) {
	c.ForEach(collections.K8SAppsV1Deployments.Name(), func(r *resource.Instance) bool {
		if util.DeploymentInMesh(r, c) {
			s.analyzeDeploymentPortProtocol(r, c)
			s.analyzeDeploymentTargetPorts(r, c)
		}
		return true
	})
}

// analyzeDeploymentPortProtocol analyzes the specific service mesh deployment
func (s *ServiceAssociationAnalyzer) analyzeDeploymentPortProtocol(r *resource.Instance, c analysis.Context) {
	// Find matching services with resulting pod from deployment
	matchingSvcs := s.findMatchingServices(r, c)

	// If there isn't any matching service, generate message: At least one service is needed.
	if len(matchingSvcs) == 0 {
		c.Report(collections.K8SAppsV1Deployments.Name(), msg.NewDeploymentRequiresServiceAssociated(r))
		return
	}

	// Generate a port map from the matching services.
	// It creates a structure that will allow us to detect
	// if there are different protocols for the same port.
	portMap := servicePortMap(matchingSvcs)

	// Determining which ports use more than one protocol.
	for port := range portMap {
		// In case there are two protocols using same port number, generate a message
		protMap := portMap[port]
		if len(protMap) > 1 {
			// Collect names from both protocols
			svcNames := make(ServiceNames, 0)
			for protocol := range protMap {
				svcNames = append(svcNames, protMap[protocol]...)
			}
			m := msg.NewDeploymentAssociatedToMultipleServices(r, r.Metadata.FullName.Name.String(), port, svcNames)

			if line, ok := util.ErrorLine(r, fmt.Sprintf(util.MetadataName)); ok {
				m.Line = line
			}

			// Reporting the message for the deployment, port and conflicting services.
			c.Report(collections.K8SAppsV1Deployments.Name(), m)
		}
	}
}

// analyzeDeploymentPortProtocol analyzes the targetPorts conflicting
func (s *ServiceAssociationAnalyzer) analyzeDeploymentTargetPorts(r *resource.Instance, c analysis.Context) {
	// Find matching services with resulting pod from deployment
	matchingSvcs := s.findMatchingServices(r, c)

	// If there isn't any matching service, generate message: At least one service is needed.
	if len(matchingSvcs) == 0 {
		c.Report(collections.K8SAppsV1Deployments.Name(), msg.NewDeploymentRequiresServiceAssociated(r))
		return
	}

	tpm := serviceTargetPortsMap(matchingSvcs)

	// Determining which ports use more than one protocol.
	for targetPort, portServices := range tpm {
		if len(portServices) > 1 {
			// Collect names from both protocols
			svcNames := make(ServiceNames, 0, len(portServices))
			ports := make([]int32, 0, len(portServices))
			for p, s := range portServices {
				svcNames = append(svcNames, s)
				ports = append(ports, p)
			}
			m := msg.NewDeploymentConflictingPorts(r, r.Metadata.FullName.Name.String(), svcNames, targetPort, ports)

			if line, ok := util.ErrorLine(r, fmt.Sprintf(util.MetadataName)); ok {
				m.Line = line
			}

			// Reporting the message for the deployment, port and conflicting services.
			c.Report(collections.K8SAppsV1Deployments.Name(), m)
		}
	}
}

// findMatchingServices returns an slice of Services that matches with deployment's pods.
func (s *ServiceAssociationAnalyzer) findMatchingServices(r *resource.Instance, c analysis.Context) []ServiceSpecWithName {
	matchingSvcs := make([]ServiceSpecWithName, 0)
	d := r.Message.(*apps_v1.DeploymentSpec)
	deploymentNS := r.Metadata.FullName.Namespace.String()

	c.ForEach(collections.K8SCoreV1Services.Name(), func(r *resource.Instance) bool {
		s := r.Message.(*core_v1.ServiceSpec)

		sSelector := k8s_labels.SelectorFromSet(s.Selector)
		pLabels := k8s_labels.Set(d.Template.Labels)
		if sSelector.Matches(pLabels) && r.Metadata.FullName.Namespace.String() == deploymentNS {
			matchingSvcs = append(matchingSvcs, ServiceSpecWithName{r.Metadata.FullName.String(), s})
		}

		return true
	})

	return matchingSvcs
}

// servicePortMap build a map of ports and protocols for each Service. e.g. m[80]["TCP"] -> svcA, svcB, svcC
func servicePortMap(svcs []ServiceSpecWithName) PortMap {
	portMap := PortMap{}

	for _, swn := range svcs {
		svc := swn.Spec
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
			portMap[sPort.Port][protocol] = append(portMap[sPort.Port][protocol], swn.Name)
		}
	}

	return portMap
}

// serviceTargetPortsMap build a map of targetPort and ports for each Service. e.g. m["80"][80] -> svc
func serviceTargetPortsMap(svcs []ServiceSpecWithName) targetPortMap {
	pm := targetPortMap{}
	for _, swn := range svcs {
		svc := swn.Spec
		for _, sPort := range svc.Ports {
			p := sPort.TargetPort.String()
			if p == "0" || p == "" {
				// By default and for convenience, the targetPort is set to the same value as the port field.
				p = strconv.Itoa(int(sPort.Port))
			}
			if _, ok := pm[p]; !ok {
				pm[p] = map[int32]string{}
			}
			pm[p][sPort.Port] = swn.Name
		}
	}
	return pm
}
