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
	"strings"

	v1 "k8s.io/api/core/v1"

	"istio.io/api/authentication/v1alpha1"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/schema/collection"
	"istio.io/istio/galley/pkg/config/schema/collections"
)

const (
	portNameHttp    = "http"
	portNameHttp2   = "http2"
	portNameHttps   = "https"
	portPrefixHttp  = portNameHttp + "-"
	portPrefixHttp2 = portNameHttp2 + "-"
	portPrefixHttps = portNameHttps + "-"
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
		Name:        "injection.JwtAnalyzer",
		Description: "Checks that jwt auth policies are against valid services",
		Inputs: collection.Names{
			collections.IstioAuthenticationV1Alpha1Policies.Name(),
			collections.K8SCoreV1Services.Name(),
		},
	}
}

func (j *JwtAnalyzer) Analyze(c analysis.Context) {
	nsm := j.buildNamespaceServiceMap(c)

	c.ForEach(collections.IstioAuthenticationV1Alpha1Policies.Name(), func(r *resource.Instance) bool {
		j.analyzeServiceTarget(r, c, nsm)
		return true
	})
}

// buildNamespaceServiceMap returns a map where the index is a namespace and the boolean
func (j *JwtAnalyzer) buildNamespaceServiceMap(ctx analysis.Context) map[string]*v1.ServiceSpec {
	// Keep track of each fqdn -> service definition
	fqdnServices := map[string]*v1.ServiceSpec{}

	ctx.ForEach(collections.K8SCoreV1Services.Name(), func(r *resource.Instance) bool {
		svcNs := r.Metadata.FullName.Namespace
		svcName := r.Metadata.FullName.Name

		svc := r.Message.(*v1.ServiceSpec)
		fqdn := util.ConvertHostToFQDN(svcNs, string(svcName))
		fqdnServices[fqdn] = svc

		return true
	})

	return fqdnServices
}

func (j *JwtAnalyzer) analyzeServiceTarget(r *resource.Instance, ctx analysis.Context, nsm map[string]*v1.ServiceSpec) {
	policy := r.Message.(*v1alpha1.Policy)
	ns := r.Metadata.FullName.Namespace

	for _, origin := range policy.Origins {
		if origin.GetJwt() != nil {
			for _, target := range policy.GetTargets() {

				fqdn := util.ConvertHostToFQDN(ns, target.GetName())
				if svc, has := nsm[fqdn]; has {
					if len(target.GetPorts()) == 0 {
						checkServicePorts(r, ctx, svc)
						continue
					}

					// check ports defined in the authentication policy for the service
					for _, port := range target.GetPorts() {
						if port.GetName() == "" {
							checkPort(r, ctx, port.GetNumber(), svc)
						}

					}
				} // else no target service was found, but not an error
			}
		}
	}

}

func checkPort(r *resource.Instance, ctx analysis.Context, portNum uint32, svc *v1.ServiceSpec) {
	var svcPort *v1.ServicePort
	for _, port := range svc.Ports {
		if strings.ToUpper(string(port.Protocol)) != "TCP" && port.Protocol != "" {
			continue
		}
		if portNum == uint32(port.Port) {
			svcPort = &port
			break
		}
	}
	if svcPort != nil {
		portName := strings.ToLower(svcPort.Name)
		if !portHasSupportedPrefix(portName) {
			ctx.Report(collections.IstioAuthenticationV1Alpha1Policies.Name(),
				msg.NewJwtFailureDueToInvalidServicePortPrefix(
					r,
					int(portNum),
					portName,
					string(svcPort.Protocol),
					svcPort.TargetPort.String(),
				))
		}

	}
}

func checkServicePorts(r *resource.Instance, ctx analysis.Context, svc *v1.ServiceSpec) {
	for _, port := range svc.Ports {
		portName := strings.ToLower(port.Name)
		if (strings.ToUpper(string(port.Protocol)) == "TCP" || port.Protocol == "") &&
			portHasSupportedPrefix(portName) {
			continue
		} else {
			ctx.Report(collections.IstioAuthenticationV1Alpha1Policies.Name(),
				msg.NewJwtFailureDueToInvalidServicePortPrefix(
					r,
					int(port.Port),
					portName,
					string(port.Protocol),
					port.TargetPort.String(),
				))

		}
	}
}

func portHasSupportedPrefix(portName string) bool {
	return strings.HasPrefix(portName, portPrefixHttp) ||
		strings.HasPrefix(portName, portPrefixHttp2) ||
		strings.HasPrefix(portName, portPrefixHttps) ||
		portName == portNameHttp || portName == portNameHttp2 || portName == portNameHttps
}
