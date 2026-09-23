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

package dns

import (
	"strings"

	dnsProto "istio.io/istio/pkg/dns/proto"
	"istio.io/istio/pkg/util/sets"
)

// FullSnapshotResourceName marks a named NDS response as an authoritative snapshot.
const FullSnapshotResourceName = "istio.io/nds/full-snapshot"

// GenerateAltHosts returns the DNS aliases for a Kubernetes name table entry.
func GenerateAltHosts(hostname string, nameinfo *dnsProto.NameTable_NameInfo, proxyNamespace, proxyDomain string,
	proxyDomainParts []string,
) sets.String {
	out := sets.New[string]()
	if strings.HasSuffix(hostname, ".") {
		return out
	}
	out.Insert(hostname + ".")
	// do not generate alt hostnames if the service is in a different domain (i.e. cluster) than the proxy
	// as we have no way to resolve conflicts on name.namespace entries across clusters of different domains
	if proxyDomain == "" || !strings.HasSuffix(hostname, proxyDomain) {
		return out
	}
	out.Insert(nameinfo.Shortname + "." + nameinfo.Namespace + ".")
	if proxyNamespace == nameinfo.Namespace {
		out.Insert(nameinfo.Shortname + ".")
	}
	// Do we need to generate entries for name.namespace.svc, name.namespace.svc.cluster, etc. ?
	// If these are not that frequently used, then not doing so here will save some space and time
	// as some people have very long proxy domains with multiple dots
	// For now, we will generate just one more domain (which is usually the .svc piece).
	out.Insert(nameinfo.Shortname + "." + nameinfo.Namespace + "." + proxyDomainParts[0] + ".")

	// Add any additional alt hostnames.
	// nolint: staticcheck
	for _, altHost := range nameinfo.AltHosts {
		out.Insert(altHost + ".")
	}
	return out
}
