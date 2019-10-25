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

package util

import (
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/resource"
)

// ServiceEntriesToFqdnMap transforms IstioNetworkingV1Alpha3Serviceentries hostnames
// to a map with ScopedFqdn key
func ServiceEntriesToFqdnMap(ctx analysis.Context) map[ScopedFqdn]*v1alpha3.ServiceEntry {
	hosts := make(map[ScopedFqdn]*v1alpha3.ServiceEntry)

	extractFn := func(r *resource.Entry) bool {
		s := r.Item.(*v1alpha3.ServiceEntry)
		ns, _ := r.Metadata.Name.InterpretAsNamespaceAndName()
		hostsNamespaceScope := ns
		if IsExportToAllNamespaces(s.ExportTo) {
			hostsNamespaceScope = ExportToAllNamespaces
		}

		for _, h := range s.GetHosts() {
			hosts[NewScopedFqdn(hostsNamespaceScope, ns, h)] = s
		}
		return true
	}

	ctx.ForEach(metadata.IstioNetworkingV1Alpha3Serviceentries, extractFn)
	ctx.ForEach(metadata.IstioNetworkingV1Alpha3SyntheticServiceentries, extractFn)

	return hosts
}
