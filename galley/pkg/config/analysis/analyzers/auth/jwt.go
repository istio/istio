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

package auth

import (
	"regexp"

	v1 "k8s.io/api/core/v1"

	"istio.io/api/authentication/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

var (
	jwtSupportedPortName = regexp.MustCompile("^http(2|s)?(-.*)?$")
)

// JwtAnalyzer checks for misconfiguration of an Authentication policy
// that specifies a JWT, but the specified target host's K8s Service definition
// does not have a named port that matches <protocol>[-<suffix>].
type JwtAnalyzer struct{}

// (compile-time check that we implement the interface)
var _ analysis.Analyzer = &JwtAnalyzer{}

// Metadata implements JwtAnalyzer
func (j *JwtAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "injection.JwtAnalyzer",
		Inputs: collection.Names{
			metadata.IstioAuthenticationV1Alpha1Policies,
			metadata.K8SCoreV1Services,
		},
	}
}

func (j *JwtAnalyzer) Analyze(c analysis.Context) {
	nsm := j.buildNamespaceServiceMap(c)

	c.ForEach(metadata.IstioAuthenticationV1Alpha1Policies, func(r *resource.Entry) bool {
		j.analyzeServiceTarget(r, c, nsm)
		return true
	})
}

// buildNamespaceServiceMap returns a map where the index is a namespace and the boolean
func (j *JwtAnalyzer) buildNamespaceServiceMap(ctx analysis.Context) map[string]*v1.ServiceSpec {
	// Keep track of each fqdn -> service definition
	fqdnServices := map[string]*v1.ServiceSpec{}

	ctx.ForEach(metadata.K8SCoreV1Services, func(r *resource.Entry) bool {
		svcNs, svcName := r.Metadata.Name.InterpretAsNamespaceAndName()

		svc := r.Item.(*v1.ServiceSpec)
		fqdn := util.ConvertHostToFQDN(svcNs, svcName)
		fqdnServices[fqdn] = svc

		return true
	})

	return fqdnServices
}

func (j *JwtAnalyzer) analyzeServiceTarget(r *resource.Entry, ctx analysis.Context, nsm map[string]*v1.ServiceSpec) {
	policy := r.Item.(*v1alpha1.Policy)
	ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()

	for _, origin := range policy.Origins {
		if origin.GetJwt() == nil {
			continue
		}

		for _, target := range policy.GetTargets() {
			fqdn := util.ConvertHostToFQDN(ns, target.GetName())
			svc, ok := nsm[fqdn]
			if !ok {
				// service was not found, but this is not considered an error
				continue
			}

			if len(target.GetPorts()) == 0 {
				checkServicePorts(r, ctx, svc)
				continue
			}

			// check ports defined in the authentication policy for the service
			for _, port := range target.GetPorts() {
				if port.GetName() == "" {
					checkPortNumber(r, ctx, port.GetNumber(), svc)
				} else {
					checkPortName(r, ctx, port.GetName(), svc)
				}
			}
		}
	}
}

func checkPortName(r *resource.Entry, ctx analysis.Context, portName string, svc *v1.ServiceSpec) {
	var svcPort *v1.ServicePort
	for _, port := range svc.Ports {
		if portName != port.Name {
			continue
		}
		if !isTCPProtocol(port.Protocol) {
			ctx.Report(metadata.IstioAuthenticationV1Alpha1Policies,
				msg.NewJwtFailureDueToInvalidServicePortPrefix(
					r,
					int(port.Port),
					port.Name,
					string(port.Protocol),
					port.TargetPort.String(),
				))
			return
		}

		svcPort = &port
		break
	}
	checkPort(r, ctx, svcPort)
}

func checkPortNumber(r *resource.Entry, ctx analysis.Context, portNum uint32, svc *v1.ServiceSpec) {
	var svcPort *v1.ServicePort
	for _, port := range svc.Ports {
		if !isTCPProtocol(port.Protocol) {
			continue
		}
		if portNum == uint32(port.Port) {
			svcPort = &port
			break
		}
	}
	checkPort(r, ctx, svcPort)
}

func checkPort(r *resource.Entry, ctx analysis.Context, svcPort *v1.ServicePort) {
	if svcPort == nil {
		return
	}

	svcPortName := svcPort.Name
	if !isJwtSupportedPortName(svcPortName) {
		ctx.Report(metadata.IstioAuthenticationV1Alpha1Policies,
			msg.NewJwtFailureDueToInvalidServicePortPrefix(
				r,
				int(svcPort.Port),
				svcPortName,
				string(svcPort.Protocol),
				svcPort.TargetPort.String(),
			))
	}
}

func checkServicePorts(r *resource.Entry, ctx analysis.Context, svc *v1.ServiceSpec) {
	for _, port := range svc.Ports {
		if isTCPProtocol(port.Protocol) && isJwtSupportedPortName(port.Name) {
			continue
		} else {
			ctx.Report(metadata.IstioAuthenticationV1Alpha1Policies,
				msg.NewJwtFailureDueToInvalidServicePortPrefix(
					r,
					int(port.Port),
					port.Name,
					string(port.Protocol),
					port.TargetPort.String(),
				))

		}
	}
}

func isTCPProtocol(protocol v1.Protocol) bool {
	return string(protocol) == "TCP" || protocol == ""
}

func isJwtSupportedPortName(portName string) bool {
	return jwtSupportedPortName.Match([]byte(portName))
}
