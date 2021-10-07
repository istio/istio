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

package virtualservice

import (
	"fmt"
	"strings"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/util"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/resource"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
)

// ConflictingMeshGatewayHostsAnalyzer checks if multiple virtual services
// associated with the mesh gateway have conflicting hosts. The behavior is
// undefined if conflicts exist.
type ConflictingMeshGatewayHostsAnalyzer struct{}

var _ analysis.Analyzer = &ConflictingMeshGatewayHostsAnalyzer{}

// Metadata implements Analyzer
func (c *ConflictingMeshGatewayHostsAnalyzer) Metadata() analysis.Metadata {
	return analysis.Metadata{
		Name:        "virtualservice.ConflictingMeshGatewayHostsAnalyzer",
		Description: "Checks if multiple virtual services associated with the mesh gateway have conflicting hosts",
		Inputs: collection.Names{
			collections.IstioNetworkingV1Alpha3Virtualservices.Name(),
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

				m := msg.NewConflictingMeshGatewayVirtualServiceHosts(vsList[i], vsNames, string(scopedFqdn))

				if line, ok := util.ErrorLine(vsList[i], fmt.Sprintf(util.MetadataName)); ok {
					m.Line = line
				}

				ctx.Report(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), m)
			}
		}
	}
}

func combineResourceEntryNames(rList map[resource.FullName]*resource.Instance) string {
	names := make([]string, 0, len(rList))
	for name := range rList {
		names = append(names, name.String())
	}
	return strings.Join(names, ",")
}

func initMeshGatewayHosts(ctx analysis.Context) map[util.ScopedFqdn]map[resource.FullName]*resource.Instance {
	hostsVirtualServices := map[util.ScopedFqdn]map[resource.FullName]*resource.Instance{}
	ctx.ForEach(collections.IstioNetworkingV1Alpha3Virtualservices.Name(), func(r *resource.Instance) bool {
		vs := r.Message.(*v1alpha3.VirtualService)
		vsNamespace := r.Metadata.FullName.Namespace
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
		if !vsAttachedToMeshGateway {
			return true
		}

		// determine the scope of hosts i.e. local to VirtualService namespace,
		// assigned to specific namespaces or all namespaces
		exportToScopes := make(map[string]bool, 0)
		if util.IsExportToAllNamespaces(vs.ExportTo) {
			exportToScopes[util.ExportToAllNamespaces] = true
		} else {
			for _, ns := range vs.ExportTo {
				if ns == util.ExportToNamespaceLocal {
					exportToScopes[string(vsNamespace)] = true
				} else {
					exportToScopes[ns] = true
				}
			}
		}

		for ns, exist := range exportToScopes {
			if !exist {
				continue
			}
			for _, h := range vs.Hosts {
				scopedFqdn := util.NewScopedFqdn(ns, vsNamespace, h)
				vsNames := hostsVirtualServices[scopedFqdn]
				if vsNames == nil || len(vsNames) == 0 {
					hostsVirtualServices[scopedFqdn] = map[resource.FullName]*resource.Instance{}
				}
				hostsVirtualServices[scopedFqdn][r.Metadata.FullName] = r
			}
		}
		return true
	})

	// translate FQDNs that are exposed to all namespaces into other namespaces,
	// and if only the scope "*" exists, keep it to record the conflict hosts.
	for host, resrouces := range hostsVirtualServices {
		scope, fqdn := host.GetScopeAndFqdn()
		if scope != util.ExportToAllNamespaces {
			continue
		}

		scopedFqdnExist := false
		for scopedHost := range hostsVirtualServices {
			newScope, newFqdn := scopedHost.GetScopeAndFqdn()
			if newScope == util.ExportToAllNamespaces {
				continue
			}
			if newFqdn != fqdn {
				continue
			}
			scopedFqdnExist = true
			for name, r := range resrouces {
				hostsVirtualServices[scopedHost][name] = r
			}
		}
		// delete */fqdn if fqdn exists in other namespaces to avoid of duplicated messages
		if scopedFqdnExist {
			delete(hostsVirtualServices, host)
		}
	}
	return hostsVirtualServices
}
