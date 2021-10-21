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
	"sort"
	"strings"
	"sync"

	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	httpwasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	wasmfilter "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/wasm/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/wasm/v3"
	"github.com/gogo/protobuf/types"
	"google.golang.org/protobuf/types/known/anypb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"

	sd "istio.io/api/envoy/extensions/stackdriver/config/v1alpha1"
	"istio.io/api/envoy/extensions/stats"
	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/util/protomarshal"
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
	namespaceToTelemetries map[string][]Telemetry

	// The name of the root namespace.
	rootNamespace string

	// Computed meshConfig
	meshConfig *meshconfig.MeshConfig

	// computedMetricsFilters contains the set of cached HCM/listener filters for the metrics portion.
	// These filters are extremely costly, as we insert them into every listener on every proxy, and to
	// generate them we need to merge many telemetry specs and perform 2 Any marshals.
	// To improve performance, we store a cache based on the Telemetries that impacted the filter, as well as
	// its class and protocol. This is protected by mu.
	// Currently, this only applies to metrics, but a similar concept can likely be applied to logging and
	// tracing for performance.
	// The computedMetricsFilters lifetime is bound to the Telemetries object. During a push context
	// creation, we will preserve the Telemetries (and thus the cache) if not Telemetries are modified.
	// As result, this cache will live until any Telemetry is modified.
	computedMetricsFilters map[metricsKey]interface{}
	mu                     sync.Mutex
}

// telemetryKey defines a key into the computedMetricsFilters cache.
type telemetryKey struct {
	// Root stores the Telemetry in the root namespace, if any
	Root NamespacedName
	// Namespace stores the Telemetry in the root namespace, if any
	Namespace NamespacedName
	// Workload stores the Telemetry in the root namespace, if any
	Workload NamespacedName
}

// metricsKey defines a key into the computedMetricsFilters cache.
type metricsKey struct {
	telemetryKey
	Class     networking.ListenerClass
	Protocol  networking.ListenerProtocol
}

// getTelemetries returns the Telemetry configurations for the given environment.
func getTelemetries(env *Environment) (*Telemetries, error) {
	telemetries := &Telemetries{
		namespaceToTelemetries: map[string][]Telemetry{},
		rootNamespace:          env.Mesh().GetRootNamespace(),
		meshConfig:             env.Mesh(),
		computedMetricsFilters: map[metricsKey]interface{}{},
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
		telemetries.namespaceToTelemetries[config.Namespace] =
			append(telemetries.namespaceToTelemetries[config.Namespace], telemetry)
	}

	return telemetries, nil
}

func (t *Telemetries) EffectiveTelemetry(proxy *Proxy) *tpb.Telemetry {
	if t == nil {
		return nil
	}

	namespace := proxy.ConfigNamespace
	workload := labels.Collection{proxy.Metadata.Labels}

	var effectiveSpec *tpb.Telemetry
	if t.rootNamespace != "" {
		effectiveSpec = t.namespaceWideTelemetry(t.rootNamespace)
	}

	if namespace != t.rootNamespace {
		nsSpec := t.namespaceWideTelemetry(namespace)
		effectiveSpec = shallowMerge(effectiveSpec, nsSpec)
	}

	for _, telemetry := range t.namespaceToTelemetries[namespace] {
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

type telemetryProvider string

type telemetryMetricsMode struct {
	Provider *meshconfig.MeshConfig_ExtensionProvider
	Client   []metricsOverride
	Server   []metricsOverride
}

type telemetryFilterConfig struct {
	Provider      *meshconfig.MeshConfig_ExtensionProvider
	ClientMetrics []metricsOverride
	ServerMetrics []metricsOverride
	Metrics       bool
	AccessLogging bool
}

func (t telemetryFilterConfig) MetricsForClass(c networking.ListenerClass) []metricsOverride {
	switch c {
	case networking.ListenerClassGateway:
		return t.ClientMetrics
	case networking.ListenerClassSidecarInbound:
		return t.ServerMetrics
	case networking.ListenerClassSidecarOutbound:
		return t.ClientMetrics
	default:
		return t.ClientMetrics
	}
}

func (t telemetryMetricsMode) ForClass(c networking.ListenerClass) []metricsOverride {
	switch c {
	case networking.ListenerClassGateway:
		return t.Client
	case networking.ListenerClassSidecarInbound:
		return t.Server
	case networking.ListenerClassSidecarOutbound:
		return t.Client
	default:
		return t.Client
	}
}

type metricsOverride struct {
	Name     string
	Disabled bool
	Tags     []tagOverride
}

type tagOverride struct {
	Name   string
	Remove bool
	Value  string
}

// HTTPMetricsFilters computes the metrics HttpFilter for a given proxy/class
func (t *Telemetries) HTTPMetricsFilters(proxy *Proxy, class networking.ListenerClass) []*hcm.HttpFilter {
	if res := t.telemetryFilters(proxy, class, networking.ListenerProtocolHTTP); res != nil {
		return res.([]*hcm.HttpFilter)
	}
	return nil
}

// TCPMetricsFilters computes the metrics TCPMetricsFilters for a given proxy/class
func (t *Telemetries) TCPMetricsFilters(proxy *Proxy, class networking.ListenerClass) []*listener.Filter {
	if res := t.telemetryFilters(proxy, class, networking.ListenerProtocolTCP); res != nil {
		return res.([]*listener.Filter)
	}
	return nil
}

type computedTelemetries struct {
	telemetryKey
	Metrics []*tpb.Metrics
	Logging []*tpb.AccessLogging
	Tracing []*tpb.Tracing
}

type TracingConfig struct {
	Provider                 *meshconfig.MeshConfig_ExtensionProvider
	Disabled                 bool
	RandomSamplingPercentage float64
	CustomTags               map[string]*tpb.Tracing_CustomTag
}

func (t *Telemetries) Tracing(proxy *Proxy) *TracingConfig {
	ct := t.compute(proxy)
	// provider -> mode -> metric -> overrides

	providerNames := t.meshConfig.GetDefaultProviders().GetTracing()
	for _, m := range ct.Tracing {
		currentNames := getProviderNames(m.Providers)
		if len(currentNames) > 0 {
			providerNames = currentNames
		}
	}
	if len(providerNames) == 0 {
		return nil
	}
	if len(providerNames) > 1 {
		log.Debugf("invalid tracing configure; only one provider supported: %v", providerNames)
	}
	supportedProvider := providerNames[0]
	cfg := TracingConfig{
		Provider: t.fetchProvider(supportedProvider),
	}
	for _, m := range ct.Tracing {

		names := getProviderNames(m.Providers)
		includeConfig := false
		if len(names) == 0 {
			includeConfig = true
		}
		for _, n := range names {
			if n == supportedProvider {
				includeConfig = true
				break
			}
		}
		if !includeConfig {
			break
		}
		if m.DisableSpanReporting != nil {
			cfg.Disabled = m.DisableSpanReporting.GetValue()
		}
		if m.CustomTags != nil {
			cfg.CustomTags = m.CustomTags
		}
		if m.RandomSamplingPercentage != nil {
			cfg.RandomSamplingPercentage = m.RandomSamplingPercentage.GetValue()
		}
	}
	return &cfg
}

func (t *Telemetries) compute(proxy *Proxy) computedTelemetries {
	if t == nil {
		return computedTelemetries{}
	}

	namespace := proxy.ConfigNamespace
	workload := labels.Collection{proxy.Metadata.Labels}
	// Order here matters. The latter elements will override the first elements
	ms := []*tpb.Metrics{}
	ls := []*tpb.AccessLogging{}
	ts := []*tpb.Tracing{}
	key := telemetryKey{}
	if t.rootNamespace != "" {
		telemetry := t.namespaceWideTelemetryConfig(t.rootNamespace)
		if telemetry != (Telemetry{}) {
			key.Root = NamespacedName{Name: telemetry.Name, Namespace: telemetry.Namespace}
			ms = append(ms, telemetry.Spec.GetMetrics()...)
			ls = append(ls, telemetry.Spec.GetAccessLogging()...)
			ts = append(ts, telemetry.Spec.GetTracing()...)
		}
	}

	if namespace != t.rootNamespace {
		telemetry := t.namespaceWideTelemetryConfig(namespace)
		if telemetry != (Telemetry{}) {
			key.Namespace = NamespacedName{Name: telemetry.Name, Namespace: telemetry.Namespace}
			ms = append(ms, telemetry.Spec.GetMetrics()...)
			ls = append(ls, telemetry.Spec.GetAccessLogging()...)
			ts = append(ts, telemetry.Spec.GetTracing()...)
		}
	}

	for _, telemetry := range t.namespaceToTelemetries[namespace] {
		spec := telemetry.Spec
		if len(spec.GetSelector().GetMatchLabels()) == 0 {
			continue
		}
		selector := labels.Instance(spec.GetSelector().GetMatchLabels())
		if workload.IsSupersetOf(selector) {
			key.Workload = NamespacedName{Name: telemetry.Name, Namespace: telemetry.Namespace}
			ms = append(ms, spec.GetMetrics()...)
			ls = append(ls, spec.GetAccessLogging()...)
			ts = append(ts, spec.GetTracing()...)
			break
		}
	}

	return computedTelemetries{
		telemetryKey: key,
		Metrics:    ms,
		Logging:    ls,
		Tracing:    ts,
	}
}

// telemetryFilters computes the metrics filters for the given proxy/class and protocol. This computes the
// set of applicable Telemetries, merges them, then translates to the appropriate filters based on the
// extension providers in the mesh config. Where possible, the result is cached.
func (t *Telemetries) telemetryFilters(proxy *Proxy, class networking.ListenerClass, protocol networking.ListenerProtocol) interface{} {
	if t == nil {
		return nil
	}

	c := t.compute(proxy)

	key := metricsKey{
		telemetryKey: c.telemetryKey,
		Class:        class,
		Protocol:     protocol,
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	precomputed, f := t.computedMetricsFilters[key]
	if f {
		return precomputed
	}

	// First, take all the metrics configs and transform them into a normalized form
	tmm := mergeMetrics(c.Metrics, t.meshConfig)
	tml := mergeLogs(c.Logging, t.meshConfig)

	// The above result is in a nested map to deduplicate responses. This loses ordering, so we convert to
	// a list to retain stable naming
	m := []telemetryFilterConfig{}
	keys := tml.UnsortedList()
	for k := range tmm {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := t.fetchProvider(k)
		if p == nil {
			continue
		}
		_, logging := tml[k]
		_, metrics := tmm[telemetryProvider(k)]
		cfg := telemetryFilterConfig{
			Provider:      p,
			ClientMetrics: tmm[telemetryProvider(k)].Client,
			ServerMetrics: tmm[telemetryProvider(k)].Server,
			AccessLogging: logging,
			Metrics:       metrics,
		}
		m = append(m, cfg)
	}

	var res interface{}
	// Finally, compute the actual filters based on the protoc
	switch protocol {
	case networking.ListenerProtocolHTTP:
		res = buildHTTPTelemetryFilter(class, m)
	default:
		res = buildTCPTelemetryFilter(class, m)
	}

	// Update cache
	t.computedMetricsFilters[key] = res
	return res
}

// mergeMetrics merges many Metrics objects into a normalized configuration
func mergeLogs(logs []*tpb.AccessLogging, mesh *meshconfig.MeshConfig) sets.Set {
	// provider -> mode -> metric -> overrides
	providers := sets.NewSet()

	if len(logs) == 0 {
		for _, dp := range mesh.GetDefaultProviders().GetAccessLogging() {
			// Insert the default provider. It has no overrides; presence of the key is sufficient to
			// get the filter created.
			providers.Insert(dp)
		}
	}

	providerNames := mesh.GetDefaultProviders().GetAccessLogging()
	for _, m := range logs {
		names := getProviderNames(m.Providers)
		if len(names) > 0 {
			providerNames = names
		}
	}
	inScopeProviders := sets.NewSet(providerNames...)
	parentProviders := mesh.GetDefaultProviders().GetAccessLogging()
	for _, m := range logs {
		providerNames := getProviderNames(m.Providers)
		if len(providerNames) == 0 {
			providerNames = parentProviders
		}
		parentProviders = providerNames
		for _, provider := range providerNames {
			if !inScopeProviders.Contains(provider) {
				// We don't care about this, remove it
				// This occurs when a top level provider is later disabled by a lower level
				continue
			}
			if m.GetDisabled().GetValue() {
				providers.Delete(provider)
				continue
			}
			providers.Insert(provider)
		}
	}

	return providers
}

func (t *Telemetries) namespaceWideTelemetryConfig(namespace string) Telemetry {
	for _, tel := range t.namespaceToTelemetries[namespace] {
		if len(tel.Spec.GetSelector().GetMatchLabels()) == 0 {
			return tel
		}
	}
	return Telemetry{}
}

func (t *Telemetries) namespaceWideTelemetry(namespace string) *tpb.Telemetry {
	for _, tel := range t.namespaceToTelemetries[namespace] {
		spec := tel.Spec
		if len(spec.GetSelector().GetMatchLabels()) == 0 {
			return spec
		}
	}
	return nil
}

// fetchProvider finds the matching ExtensionProviders from the mesh config
func (t *Telemetries) fetchProvider(m string) *meshconfig.MeshConfig_ExtensionProvider {
	for _, p := range t.meshConfig.ExtensionProviders {
		if strings.EqualFold(m, p.Name) {
			return p
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
	shallowMergeAccessLogs(merged, child)
	return merged
}

var allMetrics = func() []string {
	r := []string{}
	for k := range tpb.MetricSelector_IstioMetric_value {
		if k != tpb.MetricSelector_IstioMetric_name[int32(tpb.MetricSelector_ALL_METRICS)] {
			r = append(r, k)
		}
	}
	sort.Strings(r)
	return r
}()

type MetricOverride struct {
	Disabled     *types.BoolValue
	TagOverrides map[string]*tpb.MetricsOverrides_TagOverride
}

// mergeMetrics merges many Metrics objects into a normalized configuration
func mergeMetrics(metrics []*tpb.Metrics, mesh *meshconfig.MeshConfig) map[telemetryProvider]telemetryMetricsMode {
	// provider -> mode -> metric -> overrides
	providers := map[telemetryProvider]map[tpb.WorkloadMode]map[string]MetricOverride{}

	if len(metrics) == 0 {
		for _, dp := range mesh.GetDefaultProviders().GetMetrics() {
			// Insert the default provider. It has no overrides; presence of the key is sufficient to
			// get the filter created.
			providers[telemetryProvider(dp)] = map[tpb.WorkloadMode]map[string]MetricOverride{}
		}
	}

	providerNames := mesh.GetDefaultProviders().GetMetrics()
	for _, m := range metrics {
		names := getProviderNames(m.Providers)
		if len(names) > 0 {
			providerNames = names
		}
	}
	inScopeProviders := sets.NewSet(providerNames...)
	parentProviders := mesh.GetDefaultProviders().GetMetrics()
	for _, m := range metrics {
		providerNames := getProviderNames(m.Providers)
		if len(providerNames) == 0 {
			providerNames = parentProviders
		}
		parentProviders = providerNames
		for _, provider := range providerNames {
			if !inScopeProviders.Contains(provider) {
				// We don't care about this, remove it
				// This occurs when a top level provider is later disabled by a lower level
				continue
			}
			p := telemetryProvider(provider)
			if _, f := providers[p]; !f {
				providers[p] = map[tpb.WorkloadMode]map[string]MetricOverride{
					tpb.WorkloadMode_CLIENT: {},
					tpb.WorkloadMode_SERVER: {},
				}
			}
			mp := providers[p]
			// For each override, we normalize the configuration. The metrics list is an ordered list - latter
			// elements have precedence. As a result, we will apply updates on top of previous entries.
			for _, o := range m.Overrides {
				// If client or server is set explicitly, only apply there. Otherwise, we will apply to both.
				// Note: client and server keys may end up the same, which is fine
				for _, mode := range getModes(o.GetMatch().GetMode()) {
					// Next, get all matches.
					// This is a bit funky because the matches are oneof of ENUM and customer metric. We normalize
					// these to strings, so we may end up with a list like [REQUEST_COUNT, my-customer-metric].
					// TODO: we always flatten ALL_METRICS into each metric mode. For some stats providers (prometheus),
					// we are able to apply overrides to all metrics directly rather than duplicating the config.
					// We should tweak this to collapse to this mode where possible
					// TODO: similar to above, if we disable all metrics, we should drop the entire filter
					for _, metricName := range getMatches(o.GetMatch()) {
						if _, f := mp[mode]; !f {
							mp[mode] = map[string]MetricOverride{}
						}
						override := mp[mode][metricName]
						if o.Disabled != nil {
							override.Disabled = o.Disabled
						}
						for k, v := range o.TagOverrides {
							if override.TagOverrides == nil {
								override.TagOverrides = map[string]*tpb.MetricsOverrides_TagOverride{}
							}
							override.TagOverrides[k] = v
						}
						mp[mode][metricName] = override
					}
				}
			}
		}
	}

	// TODO: we are doing 3 layers of processing. This layer is likely excessive, we can consolidate it with
	// the list flattening.
	processed := map[telemetryProvider]telemetryMetricsMode{}
	for provider, modeMap := range providers {
		for mode, metricMap := range modeMap {
			for metric, override := range metricMap {
				tags := []tagOverride{}
				for k, v := range override.TagOverrides {
					o := tagOverride{Name: k}
					switch v.Operation {
					case tpb.MetricsOverrides_TagOverride_REMOVE:
						o.Remove = true
						o.Value = ""
					case tpb.MetricsOverrides_TagOverride_UPSERT:
						o.Value = v.GetValue()
						o.Remove = false
					}
					tags = append(tags, o)
				}
				// Keep order deterministic
				sort.Slice(tags, func(i, j int) bool {
					return tags[i].Name < tags[j].Name
				})
				mo := metricsOverride{
					Name:     metric,
					Disabled: override.Disabled.GetValue(),
					Tags:     tags,
				}
				tmm := processed[provider]
				switch mode {
				case tpb.WorkloadMode_CLIENT:
					tmm.Client = append(tmm.Client, mo)
				default:
					tmm.Server = append(tmm.Server, mo)
				}
				processed[provider] = tmm
			}
		}

		// Keep order deterministic
		tmm := processed[provider]
		sort.Slice(tmm.Server, func(i, j int) bool {
			return tmm.Server[i].Name < tmm.Server[j].Name
		})
		sort.Slice(tmm.Client, func(i, j int) bool {
			return tmm.Client[i].Name < tmm.Client[j].Name
		})
		processed[provider] = tmm
	}
	return processed
}

func getProviderNames(providers []*tpb.ProviderRef) []string {
	res := make([]string, 0, len(providers))
	for _, p := range providers {
		res = append(res, p.GetName())
	}
	return res
}

func getModes(mode tpb.WorkloadMode) []tpb.WorkloadMode {
	switch mode {
	case tpb.WorkloadMode_CLIENT, tpb.WorkloadMode_SERVER:
		return []tpb.WorkloadMode{mode}
	default:
		return []tpb.WorkloadMode{tpb.WorkloadMode_CLIENT, tpb.WorkloadMode_SERVER}
	}
}

func getMatches(match *tpb.MetricSelector) []string {
	switch m := match.GetMetricMatch().(type) {
	case *tpb.MetricSelector_CustomMetric:
		return []string{m.CustomMetric}
	case *tpb.MetricSelector_Metric:
		if m.Metric == tpb.MetricSelector_ALL_METRICS {
			return allMetrics
		}
		return []string{m.Metric.String()}
	default:
		return allMetrics
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

const (
	statsFilterName       = "istio.stats"
	stackdriverFilterName = "istio.stackdriver"
)

func statsRootIDForClass(class networking.ListenerClass) string {
	switch class {
	case networking.ListenerClassSidecarInbound:
		return "stats_inbound"
	default:
		return "stats_outbound"
	}
}

func buildHTTPTelemetryFilter(class networking.ListenerClass, filterConfigs []telemetryFilterConfig) []*hcm.HttpFilter {
	res := []*hcm.HttpFilter{}
	for _, cfg := range filterConfigs {
		switch cfg.Provider.GetProvider().(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_Prometheus:
			if !cfg.Metrics {
				// No logging for prometheus
				continue
			}
			cfg := generateStatsConfig(class, cfg)
			vmConfig := ConstructVMConfig("/etc/istio/extensions/stats-filter.compiled.wasm", "envoy.wasm.stats")
			root := statsRootIDForClass(class)
			vmConfig.VmConfig.VmId = root

			wasmConfig := &httpwasm.Wasm{
				Config: &wasm.PluginConfig{
					RootId:        root,
					Vm:            vmConfig,
					Configuration: cfg,
				},
			}

			f := &hcm.HttpFilter{
				Name:       statsFilterName,
				ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: networking.MessageToAny(wasmConfig)},
			}
			res = append(res, f)
		case *meshconfig.MeshConfig_ExtensionProvider_Stackdriver:
			cfg := generateSDConfig(class, cfg)
			vmConfig := ConstructVMConfig("", "envoy.wasm.null.stackdriver")
			vmConfig.VmConfig.VmId = stackdriverVMID(class)

			wasmConfig := &httpwasm.Wasm{
				Config: &wasm.PluginConfig{
					RootId:        vmConfig.VmConfig.VmId,
					Vm:            vmConfig,
					Configuration: cfg,
				},
			}

			f := &hcm.HttpFilter{
				Name:       stackdriverFilterName,
				ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: networking.MessageToAny(wasmConfig)},
			}
			res = append(res, f)
		default:
			// Only prometheus and SD supported currently
			continue
		}
	}
	return res
}

func buildTCPTelemetryFilter(class networking.ListenerClass, telemetryConfigs []telemetryFilterConfig) []*listener.Filter {
	res := []*listener.Filter{}
	for _, telemetryCfg := range telemetryConfigs {
		switch telemetryCfg.Provider.GetProvider().(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_Prometheus:
			cfg := generateStatsConfig(class, telemetryCfg)
			vmConfig := ConstructVMConfig("/etc/istio/extensions/stats-filter.compiled.wasm", "envoy.wasm.stats")
			root := statsRootIDForClass(class)
			vmConfig.VmConfig.VmId = "tcp_" + root

			wasmConfig := &wasmfilter.Wasm{
				Config: &wasm.PluginConfig{
					RootId:        root,
					Vm:            vmConfig,
					Configuration: cfg,
				},
			}

			f := &listener.Filter{
				Name:       statsFilterName,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: networking.MessageToAny(wasmConfig)},
			}
			res = append(res, f)
		case *meshconfig.MeshConfig_ExtensionProvider_Stackdriver:
			cfg := generateSDConfig(class, telemetryCfg)
			vmConfig := ConstructVMConfig("", "envoy.wasm.null.stackdriver")
			vmConfig.VmConfig.VmId = stackdriverVMID(class)

			wasmConfig := &wasmfilter.Wasm{
				Config: &wasm.PluginConfig{
					RootId:        vmConfig.VmConfig.VmId,
					Vm:            vmConfig,
					Configuration: cfg,
				},
			}

			f := &listener.Filter{
				Name:       stackdriverFilterName,
				ConfigType: &listener.Filter_TypedConfig{TypedConfig: networking.MessageToAny(wasmConfig)},
			}
			res = append(res, f)
		default:
			// Only prometheus and SD supported currently
			continue
		}
	}
	return res
}

func stackdriverVMID(class networking.ListenerClass) string {
	switch class {
	case networking.ListenerClassSidecarInbound:
		return "stackdriver_inbound"
	default:
		return "stackdriver_outbound"
	}
}

var metricToSDServerMetrics = map[string]string{
	"REQUEST_COUNT":          "server/request_count",
	"REQUEST_DURATION":       "server/response_latencies",
	"REQUEST_SIZE":           "server/request_bytes",
	"RESPONSE_SIZE":          "server/response_bytes",
	"TCP_OPENED_CONNECTIONS": "server/connection_open_count",
	"TCP_CLOSED_CONNECTIONS": "server/connection_close_count",
	"TCP_SENT_BYTES":         "server/sent_bytes_count",
	"TCP_RECEIVED_BYTES":     "server/received_bytes_count",
	"GRPC_REQUEST_MESSAGES":  "",
	"GRPC_RESPONSE_MESSAGES": "",
}

var metricToSDClientMetrics = map[string]string{
	"REQUEST_COUNT":          "client/request_count",
	"REQUEST_DURATION":       "client/response_latencies",
	"REQUEST_SIZE":           "client/request_bytes",
	"RESPONSE_SIZE":          "client/response_bytes",
	"TCP_OPENED_CONNECTIONS": "client/connection_open_count",
	"TCP_CLOSED_CONNECTIONS": "client/connection_close_count",
	"TCP_SENT_BYTES":         "client/sent_bytes_count",
	"TCP_RECEIVED_BYTES":     "client/received_bytes_count",
	"GRPC_REQUEST_MESSAGES":  "",
	"GRPC_RESPONSE_MESSAGES": "",
}

func generateSDConfig(class networking.ListenerClass, telemetryConfig telemetryFilterConfig) *anypb.Any {
	cfg := sd.PluginConfig{
		DisableHostHeaderFallback: disableHostHeaderFallback(class),
	}
	merticNameMap := metricToSDClientMetrics
	if class == networking.ListenerClassSidecarInbound {
		merticNameMap = metricToSDServerMetrics
	}
	for _, override := range telemetryConfig.MetricsForClass(class) {
		metricName, f := merticNameMap[override.Name]
		if !f {
			// Not a predefined metric, must be a custom one
			metricName = override.Name
		}
		if metricName == "" {
			continue
		}
		if cfg.MetricsOverrides == nil {
			cfg.MetricsOverrides = map[string]*sd.MetricsOverride{}
		}
		if _, f := cfg.MetricsOverrides[metricName]; !f {
			cfg.MetricsOverrides[metricName] = &sd.MetricsOverride{}
		}
		cfg.MetricsOverrides[metricName].Drop = override.Disabled
		for _, t := range override.Tags {
			if t.Remove {
				// Remove is not supported by SD
				continue
			}
			if cfg.MetricsOverrides[metricName].TagOverrides == nil {
				cfg.MetricsOverrides[metricName].TagOverrides = map[string]string{}
			}
			cfg.MetricsOverrides[metricName].TagOverrides[t.Name] = t.Value
		}
	}
	if telemetryConfig.AccessLogging {
		// TODO: currently we cannot configure this granularity in the API, so we fallback to common defaults.
		if class == networking.ListenerClassSidecarInbound {
			cfg.AccessLogging = sd.PluginConfig_FULL
		} else {
			cfg.AccessLogging = sd.PluginConfig_ERRORS_ONLY
		}
	}
	// In WASM we are not actually processing protobuf at all, so we need to encode this to JSON
	cfgJSON, _ := protomarshal.MarshalProtoNames(&cfg)
	return networking.MessageToAny(&wrappers.StringValue{Value: string(cfgJSON)})
}

var metricToPrometheusMetric = map[string]string{
	"REQUEST_COUNT":          "requests_total",
	"REQUEST_DURATION":       "request_duration_milliseconds",
	"REQUEST_SIZE":           "request_bytes",
	"RESPONSE_SIZE":          "response_bytes",
	"TCP_OPENED_CONNECTIONS": "tcp_connections_opened_total",
	"TCP_CLOSED_CONNECTIONS": "tcp_connections_closed_total",
	"TCP_SENT_BYTES":         "tcp_sent_bytes_total",
	"TCP_RECEIVED_BYTES":     "tcp_received_bytes_total",
	"GRPC_REQUEST_MESSAGES":  "request_messages_total",
	"GRPC_RESPONSE_MESSAGES": "response_messages_total",
}

func generateStatsConfig(class networking.ListenerClass, metricsCfg telemetryFilterConfig) *anypb.Any {
	cfg := stats.PluginConfig{
		DisableHostHeaderFallback: disableHostHeaderFallback(class),
	}
	for _, override := range metricsCfg.MetricsForClass(class) {
		metricName, f := metricToPrometheusMetric[override.Name]
		if !f {
			// Not a predefined metric, must be a custom one
			metricName = override.Name
		}
		mc := &stats.MetricConfig{
			Dimensions: map[string]string{},
			Name:       metricName,
			Drop:       override.Disabled,
		}
		for _, t := range override.Tags {
			if t.Remove {
				mc.TagsToRemove = append(mc.TagsToRemove, t.Name)
			} else {
				mc.Dimensions[t.Name] = t.Value
			}
		}
		cfg.Metrics = append(cfg.Metrics, mc)
	}
	// In WASM we are not actually processing protobuf at all, so we need to encode this to JSON
	cfgJSON, _ := protomarshal.MarshalProtoNames(&cfg)
	return networking.MessageToAny(&wrappers.StringValue{Value: string(cfgJSON)})
}

func disableHostHeaderFallback(class networking.ListenerClass) bool {
	return class == networking.ListenerClassSidecarInbound || class == networking.ListenerClassGateway
}
