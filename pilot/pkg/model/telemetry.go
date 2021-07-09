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
	"strings"

	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	istiolog "istio.io/pkg/log"
)

var telemetryLog = istiolog.RegisterScope("telemetry", "Istio Telemetry", 0)

// Telemetry holds configuration for Telemetry API resources.
type Telemetry struct {
	Name      string         `json:"name"`
	Namespace string         `json:"namespace"`
	Spec      *tpb.Telemetry `json:"spec"`
}

// Telemetries organizes Telemetry configuration by namespace.
type Telemetries struct {
	// Maps from namespace to the Telemetry configs.
	NamespaceToTelemetries map[string][]Telemetry `json:"namespace_to_telemetries"`

	// The name of the root namespace.
	RootNamespace string `json:"root_namespace"`
}

// GetTelemetries returns the Telemetry configurations for the given environment.
func GetTelemetries(env *Environment) (*Telemetries, error) {
	telemetries := &Telemetries{
		NamespaceToTelemetries: map[string][]Telemetry{},
		RootNamespace:          env.Mesh().GetRootNamespace(),
	}

	fromEnv, err := env.List(collections.IstioTelemetryV1Alpha1Telemetries.Resource().GroupVersionKind(), NamespaceAll)
	if err != nil {
		return nil, err
	}
	sortConfigByCreationTime(fromEnv)
	for _, config := range fromEnv {
		telemetry := Telemetry{
			Name:      config.Name,
			Namespace: config.Namespace,
			Spec:      config.Spec.(*tpb.Telemetry),
		}
		telemetries.NamespaceToTelemetries[config.Namespace] =
			append(telemetries.NamespaceToTelemetries[config.Namespace], telemetry)
	}

	return telemetries, nil
}

// AnyTelemetryExists determines if there are an Telemetries present in the entire mesh.
func (t *Telemetries) AnyTelemetryExists() bool {
	return len(t.NamespaceToTelemetries) > 0
}

func (t *Telemetries) EffectiveTelemetry(proxy *Proxy) *tpb.Telemetry {
	if t == nil {
		return nil
	}

	namespace := proxy.ConfigNamespace
	workload := labels.Collection{proxy.Metadata.Labels}

	var effectiveSpec *tpb.Telemetry
	if t.RootNamespace != "" {
		effectiveSpec = t.namespaceWideTelemetry(t.RootNamespace)
	}

	if namespace != t.RootNamespace {
		nsSpec := t.namespaceWideTelemetry(namespace)
		effectiveSpec = shallowMerge(effectiveSpec, nsSpec)
	}

	for _, telemetry := range t.NamespaceToTelemetries[namespace] {
		spec := telemetry.Spec
		if len(spec.GetSelector().GetMatchLabels()) == 0 {
			continue
		}
		selector := labels.Instance(spec.GetSelector().GetMatchLabels())
		if workload.IsSupersetOf(selector) {
			effectiveSpec = shallowMerge(effectiveSpec, spec)
			break
		}
	}

	return effectiveSpec
}

func (t *Telemetries) namespaceWideTelemetry(namespace string) *tpb.Telemetry {
	for _, tel := range t.NamespaceToTelemetries[namespace] {
		spec := tel.Spec
		if len(spec.GetSelector().GetMatchLabels()) == 0 {
			return spec
		}
	}
	return nil
}

func shallowMerge(parent, child *tpb.Telemetry) *tpb.Telemetry {
	if parent == nil {
		return child
	}
	if child == nil {
		return parent
	}
	merged := parent.DeepCopy()
	shallowMergeTracing(merged, child)
	mergeMetrics(merged, child)
	shallowMergeAccessLogs(merged, child)
	return merged
}

func mergeMetrics(parent *tpb.Telemetry, child *tpb.Telemetry) {
	if len(parent.GetMetrics()) == 0 {
		parent.Metrics = child.Metrics
		return
	}
	if len(child.GetMetrics()) == 0 {
		return
	}

	// TODO proper merge of matches
	childMetrics := child.Metrics[0]
	mergedMetrics := parent.Metrics[0]
	if len(childMetrics.Providers) != 0 {
		mergedMetrics.Providers = childMetrics.Providers
	}

	// TODO deep merge
	if childMetrics.Overrides != nil {
		mergedMetrics.Overrides = childMetrics.Overrides
	}
}

func shallowMergeTracing(parent, child *tpb.Telemetry) {
	if len(parent.GetTracing()) == 0 {
		parent.Tracing = child.Tracing
		return
	}
	if len(child.GetTracing()) == 0 {
		return
	}

	// only use the first Tracing for now (all that is supported)
	childTracing := child.Tracing[0]
	mergedTracing := parent.Tracing[0]
	if len(childTracing.Providers) != 0 {
		mergedTracing.Providers = childTracing.Providers
	}

	if childTracing.GetCustomTags() != nil {
		mergedTracing.CustomTags = childTracing.CustomTags
	}

	if childTracing.GetDisableSpanReporting() != nil {
		mergedTracing.DisableSpanReporting = childTracing.DisableSpanReporting
	}

	if childTracing.GetRandomSamplingPercentage() != nil {
		mergedTracing.RandomSamplingPercentage = childTracing.RandomSamplingPercentage
	}
}

func shallowMergeAccessLogs(parent *tpb.Telemetry, child *tpb.Telemetry) {
	if len(parent.GetAccessLogging()) == 0 {
		parent.AccessLogging = child.AccessLogging
		return
	}
	if len(child.GetAccessLogging()) == 0 {
		return
	}

	// Only use the first AccessLogging for now (all that is supported)
	childLogging := child.AccessLogging[0]
	mergedLogging := parent.AccessLogging[0]
	if len(childLogging.Providers) != 0 {
		mergedLogging.Providers = childLogging.Providers
	}

	if childLogging.GetDisabled() != nil {
		mergedLogging.Disabled = childLogging.Disabled
	}
}

// PrometheusEnabled determines if any of the extension providers are for prometheus
func PrometheusEnabled(providers []*meshconfig.MeshConfig_ExtensionProvider) bool {
	for _, prov := range providers {
		if _, ok := prov.Provider.(*meshconfig.MeshConfig_ExtensionProvider_Prometheus); ok {
			return true
		}
	}
	return false
}

// MetricsEnabled determines if there is any `metrics` configuration for the provided mesh configuration and Telemetry.
// Note that this can return `true` even when `spec` is `nil`, in the case there is a default provider.
func MetricsEnabled(spec *tpb.Telemetry, mesh *meshconfig.MeshConfig) bool {
	return len(MetricsProviders(spec, mesh)) > 0
}

// MetricsProviders returns a list of all metrics extension providers used by the spec
// (`metrics.providers`) and mesh configuration (`defaultProviders.metrics`).
func MetricsProviders(spec *tpb.Telemetry, mesh *meshconfig.MeshConfig) []*meshconfig.MeshConfig_ExtensionProvider {
	providers := sets.NewSet(mesh.GetDefaultProviders().GetMetrics()...)
	if len(spec.GetMetrics()) > 0 {
		// handle multiple
		if prov := spec.Metrics[0].GetProviders(); len(prov) > 0 {
			providers.Insert(strings.ToLower(prov[0].Name))
		}
	}
	if len(providers) == 0 {
		return nil
	}

	ret := []*meshconfig.MeshConfig_ExtensionProvider{}

	for _, p := range mesh.ExtensionProviders {
		name := strings.ToLower(p.Name)
		if providers.Contains(name) {
			ret = append(ret, p)
		}
	}
	return ret
}
