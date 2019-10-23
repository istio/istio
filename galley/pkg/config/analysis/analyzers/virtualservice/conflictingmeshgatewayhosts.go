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

package virtualservice

import (
	"strings"

	"istio.io/api/networking/v1alpha3"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
	"istio.io/istio/galley/pkg/config/resource"
)

// ConflictingMeshGatewayHostsAnalyzer checks if multiple VirtualServices associated
// with mesh gateway have conflicting hosts. The behavior is undefined if
// conflicts exist.
type ConflictingMeshGatewayHostsAnalyzer struct{}

var _ analysis.Analyzer = &ConflictingMeshGatewayHostsAnalyzer{}

// Metadata implements Analyzer
func (c *ConflictingMeshGatewayHostsAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name: "virtualservice.ConflictingMeshGatewayHostsAnalyzer",
		Inputs: collection.Names{
			metadata.IstioNetworkingV1Alpha3Virtualservices,
		},
	}
}

// Analyze implements Analyzer
func (c *ConflictingMeshGatewayHostsAnalyzer) Analyze(ctx analysis.Context) {
	hs := initMeshGatewayHosts(ctx)
	for scopedFqdn, vsList := range hs {
		if len(vsList) > 1 {
			vsNames := combineResourceEntryNames(vsList)
			for i := range vsList {
				ctx.Report(metadata.IstioNetworkingV1Alpha3Virtualservices,
					msg.NewConflictingMeshGatewayVirtualServiceHosts(vsList[i], vsNames, string(scopedFqdn)))
			}
		}
	}
}

func combineResourceEntryNames(rList []*resource.Entry) string {
	names := []string{}
	for _, r := range rList {
		names = append(names, r.Metadata.Name.String())
	}
	return strings.Join(names, ",")
}

func initMeshGatewayHosts(ctx analysis.Context) map[util.ScopedFqdn][]*resource.Entry {
	hostsVirtualServices := map[util.ScopedFqdn][]*resource.Entry{}
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Virtualservices, func(r *resource.Entry) bool {
		vs := r.Item.(*v1alpha3.VirtualService)
		vsNamespace, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		vsAttachedToMeshGateway := false
		// No entry in gateways imply "mesh" by default
		if len(vs.Gateways) == 0 {
			vsAttachedToMeshGateway = true
		} else {
			for _, g := range vs.Gateways {
				if g == util.MeshGateway {
					vsAttachedToMeshGateway = true
				}
			}
		}
		if vsAttachedToMeshGateway {
			// determine the scope of hosts i.e. local to VirtualService namespace or
			// all namespaces
			hostsNamespaceScope := vsNamespace
			exportToAllNamespaces := util.IsExportToAllNamespaces(vs.ExportTo)
			if exportToAllNamespaces {
				hostsNamespaceScope = util.ExportToAllNamespaces
			}

			for _, h := range vs.Hosts {
				scopedFqdn := util.GetScopedFqdnHostname(hostsNamespaceScope, vsNamespace, h)
				vsNames := hostsVirtualServices[scopedFqdn]
				if len(vsNames) == 0 {
					hostsVirtualServices[scopedFqdn] = []*resource.Entry{r}
				} else {
					hostsVirtualServices[scopedFqdn] = append(hostsVirtualServices[scopedFqdn], r)
				}
			}
		}
		return true
	})
	return hostsVirtualServices
}
