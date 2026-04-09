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

package v1alpha3

import (
	"strings"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/log"
)

var shortNameLog = log.RegisterScope("shortname", "short name resolution", 0)

// ShortNameResolver resolves short service names to FQDNs in a namespace-aware manner.
type ShortNameResolver struct {
	push *model.PushContext
}

// NewShortNameResolver creates a new resolver with access to service discovery.
func NewShortNameResolver(push *model.PushContext) *ShortNameResolver {
	return &ShortNameResolver{
		push: push,
	}
}

// ResolveHost resolves a host name, expanding short names to FQDNs if needed.
// Returns the resolved hostname and a boolean indicating if resolution was performed.
func (r *ShortNameResolver) ResolveHost(hostname string, namespace string) (string, bool) {
	if !features.EnableShortNameResolution {
		return hostname, false
	}

	// If already a FQDN (contains dots), return as-is
	if strings.Contains(hostname, ".") {
		return hostname, false
	}

	// If it's the mesh gateway constant, return as-is
	if hostname == "mesh" || hostname == "*" {
		return hostname, false
	}

	// Resolve short name to FQDN
	fqdn := r.expandToFQDN(hostname, namespace)
	if fqdn != hostname {
		shortNameLog.Debugf("Resolved short name %q in namespace %q to FQDN %q", hostname, namespace, fqdn)
		return fqdn, true
	}

	return hostname, false
}

// expandToFQDN expands a short name to FQDN using the provided namespace.
func (r *ShortNameResolver) expandToFQDN(shortName, namespace string) string {
	if namespace == "" {
		shortNameLog.Warnf("Cannot resolve short name %q without namespace context", shortName)
		return shortName
	}

	// Format: {name}.{namespace}.svc.cluster.local
	fqdn := host.Name(shortName + "." + namespace + ".svc.cluster.local")

	// Validate that the service exists in service discovery
	if r.push != nil {
		svc := r.push.GetService(fqdn)
		if svc == nil {
			shortNameLog.Warnf("Service %q not found in service discovery, but continuing with FQDN expansion", fqdn)
		}
	}

	return string(fqdn)
}

// ResolveHostWithTargetNamespace resolves a host name with an optional target namespace override.
// This is used for cross-namespace routing via x-target-namespace header.
func (r *ShortNameResolver) ResolveHostWithTargetNamespace(hostname, sourceNamespace, targetNamespace string) (string, bool) {
	if !features.EnableShortNameResolution {
		return hostname, false
	}

	// If already a FQDN, return as-is
	if strings.Contains(hostname, ".") {
		return hostname, false
	}

	// Use target namespace if provided, otherwise use source namespace
	namespace := sourceNamespace
	if targetNamespace != "" {
		namespace = targetNamespace
		shortNameLog.Debugf("Using target namespace %q for resolving %q", targetNamespace, hostname)
	}

	return r.ResolveHost(hostname, namespace)
}

// ResolveBatch resolves multiple hostnames in a single call for efficiency.
func (r *ShortNameResolver) ResolveBatch(hostnames []string, namespace string) ([]string, bool) {
	if !features.EnableShortNameResolution {
		return hostnames, false
	}

	resolved := make([]string, len(hostnames))
	anyResolved := false

	for i, h := range hostnames {
		resolved[i], wasResolved := r.ResolveHost(h, namespace)
		if wasResolved {
			anyResolved = true
		}
	}

	return resolved, anyResolved
}
