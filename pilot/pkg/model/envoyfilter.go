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
package model

import (
	"regexp"
	"strings"

	"github.com/gogo/protobuf/proto"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/xds"
)

// EnvoyFilterWrapper is a wrapper for the EnvoyFilter api object with pre-processed data
type EnvoyFilterWrapper struct {
	workloadSelector labels.Instance
	Patches          map[networking.EnvoyFilter_ApplyTo][]*EnvoyFilterConfigPatchWrapper
}

// EnvoyFilterConfigPatchWrapper is a wrapper over the EnvoyFilter ConfigPatch api object
// fields are ordered such that this struct is aligned
type EnvoyFilterConfigPatchWrapper struct {
	Value     proto.Message
	Match     *networking.EnvoyFilter_EnvoyConfigObjectMatch
	ApplyTo   networking.EnvoyFilter_ApplyTo
	Operation networking.EnvoyFilter_Patch_Operation
	// Pre-compile the regex from proxy version match in the match
	ProxyVersionRegex *regexp.Regexp
	// ProxyPrefixMatch provides a prefix match for the proxy version. The current API only allows
	// regex match, but as an optimization we can reduce this to a prefix match for common cases.
	// If this is set, ProxyVersionRegex is ignored.
	ProxyPrefixMatch string
}

// wellKnownVersions defines a mapping of well known regex matches to prefix matches
// This is done only as an optimization; behavior should remain the same
// All versions specified by the default installation (Telemetry V2) should be added here.
var wellKnownVersions = map[string]string{
	`^1\.4.*`: "1.4",
	`^1\.5.*`: "1.5",
	`^1\.6.*`: "1.6",
	`^1\.7.*`: "1.7",
	`^1\.8.*`: "1.8",
	// Hopefully we have a better API by 1.9. If not, add it here
}

// convertToEnvoyFilterWrapper converts from EnvoyFilter config to EnvoyFilterWrapper object
func convertToEnvoyFilterWrapper(local *Config) *EnvoyFilterWrapper {
	localEnvoyFilter := local.Spec.(*networking.EnvoyFilter)

	out := &EnvoyFilterWrapper{}
	if localEnvoyFilter.WorkloadSelector != nil {
		out.workloadSelector = localEnvoyFilter.WorkloadSelector.Labels
	}
	out.Patches = make(map[networking.EnvoyFilter_ApplyTo][]*EnvoyFilterConfigPatchWrapper)
	for _, cp := range localEnvoyFilter.ConfigPatches {
		cpw := &EnvoyFilterConfigPatchWrapper{
			ApplyTo:   cp.ApplyTo,
			Match:     cp.Match,
			Operation: cp.Patch.Operation,
		}
		var err error
		cpw.Value, err = xds.BuildXDSObjectFromStruct(cp.ApplyTo, cp.Patch.Value)
		// There generally won't be an error here because validation catches mismatched types
		// Should only happen in tests or without validation
		if err != nil {
			log.Errorf("failed to build envoy filter value: %v", err)
		}
		if cp.Match == nil {
			// create a match all object
			cpw.Match = &networking.EnvoyFilter_EnvoyConfigObjectMatch{Context: networking.EnvoyFilter_ANY}
		} else if cp.Match.Proxy != nil && cp.Match.Proxy.ProxyVersion != "" {
			// Attempt to convert regex to a simple prefix match for the common case of matching
			// a standard Istio version. This field should likely be replaced with semver, but for now
			// we can workaround the performance impact of regex
			if prefix, f := wellKnownVersions[cp.Match.Proxy.ProxyVersion]; f {
				cpw.ProxyPrefixMatch = prefix
			} else {
				// pre-compile the regex for proxy version if it exists
				// ignore the error because validation catches invalid regular expressions.
				cpw.ProxyVersionRegex, _ = regexp.Compile(cp.Match.Proxy.ProxyVersion)
			}
		}

		if _, exists := out.Patches[cp.ApplyTo]; !exists {
			out.Patches[cp.ApplyTo] = make([]*EnvoyFilterConfigPatchWrapper, 0)
		}
		if cpw.Operation == networking.EnvoyFilter_Patch_INSERT_AFTER ||
			cpw.Operation == networking.EnvoyFilter_Patch_INSERT_BEFORE ||
			cpw.Operation == networking.EnvoyFilter_Patch_INSERT_FIRST {
			// insert_before, after or first is applicable only for network filter and http filter
			// TODO: insert before/after is also applicable to http_routes
			// convert the rest to add
			if cpw.ApplyTo != networking.EnvoyFilter_HTTP_FILTER && cpw.ApplyTo != networking.EnvoyFilter_NETWORK_FILTER {
				cpw.Operation = networking.EnvoyFilter_Patch_ADD
			}
		}
		out.Patches[cp.ApplyTo] = append(out.Patches[cp.ApplyTo], cpw)
	}
	return out
}

func proxyMatch(proxy *Proxy, cp *EnvoyFilterConfigPatchWrapper) bool {
	if cp.Match.Proxy == nil {
		return true
	}

	if cp.ProxyPrefixMatch != "" {
		if !strings.HasPrefix(proxy.Metadata.IstioVersion, cp.ProxyPrefixMatch) {
			return false
		}
	}
	if cp.ProxyVersionRegex != nil {
		ver := proxy.Metadata.IstioVersion
		if ver == "" {
			// we do not have a proxy version but the user has a regex. so this is a mismatch
			return false
		}
		if !cp.ProxyVersionRegex.MatchString(ver) {
			return false
		}
	}

	for k, v := range cp.Match.Proxy.Metadata {
		if proxy.Metadata.Raw[k] != v {
			return false
		}
	}
	return true
}
