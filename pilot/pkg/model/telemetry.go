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
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	wrappers "google.golang.org/protobuf/types/known/wrapperspb"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/envoy/extensions/stats"
	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/xds"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/util/sets"
)

// Telemetry holds configuration for Telemetry API resources.
type Telemetry struct {
	Name      string         `json:"name"`
	Namespace string         `json:"namespace"`
	Spec      *tpb.Telemetry `json:"spec"`
}

func (t *Telemetry) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: t.Name, Namespace: t.Namespace}
}

// Telemetries organizes Telemetry configuration by namespace.
type Telemetries struct {
	// Maps from namespace to the Telemetry configs.
	NamespaceToTelemetries map[string][]Telemetry `json:"namespace_to_telemetries"`

	// The name of the root namespace.
	RootNamespace string `json:"root_namespace"`

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
	computedMetricsFilters map[metricsKey]any
	computedLoggingConfig  map[loggingKey][]LoggingConfig
	mu                     sync.Mutex
}

// telemetryKey defines a key into the computedMetricsFilters cache.
type telemetryKey struct {
	// Root stores the Telemetry in the root namespace, if any
	Root types.NamespacedName
	// Namespace stores the Telemetry in the root namespace, if any
	Namespace types.NamespacedName
	// Workload stores the Telemetry in the root namespace, if any
	Workload types.NamespacedName
}

// loggingKey defines a key into the computedLoggingConfig cache.
type loggingKey struct {
	telemetryKey
	Class    networking.ListenerClass
	Protocol networking.ListenerProtocol
	Version  string
}

// metricsKey defines a key into the computedMetricsFilters cache.
type metricsKey struct {
	telemetryKey
	Class     networking.ListenerClass
	Protocol  networking.ListenerProtocol
	ProxyType NodeType
	Service   types.NamespacedName
}

// getTelemetries returns the Telemetry configurations for the given environment.
func getTelemetries(env *Environment) *Telemetries {
	telemetries := &Telemetries{
		NamespaceToTelemetries: map[string][]Telemetry{},
		RootNamespace:          env.Mesh().GetRootNamespace(),
		meshConfig:             env.Mesh(),
		computedMetricsFilters: map[metricsKey]any{},
		computedLoggingConfig:  map[loggingKey][]LoggingConfig{},
	}

	fromEnv := env.List(gvk.Telemetry, NamespaceAll)
	sortConfigByCreationTime(fromEnv)
	for _, config := range fromEnv {
		telemetry := Telemetry{
			Name:      config.Name,
			Namespace: config.Namespace,
			Spec:      config.Spec.(*tpb.Telemetry),
		}
		telemetries.NamespaceToTelemetries[config.Namespace] = append(telemetries.NamespaceToTelemetries[config.Namespace], telemetry)
	}

	return telemetries
}

type metricsConfig struct {
	ClientMetrics            metricConfig
	ServerMetrics            metricConfig
	ReportingInterval        *durationpb.Duration
	RotationInterval         *durationpb.Duration
	GracefulDeletionInterval *durationpb.Duration
}

type metricConfig struct {
	// if true, do not add filter to chain
	Disabled  bool
	Overrides []metricsOverride
}

type telemetryFilterConfig struct {
	metricsConfig
	Provider      *meshconfig.MeshConfig_ExtensionProvider
	Metrics       bool
	AccessLogging bool
	LogsFilter    *tpb.AccessLogging_Filter
	NodeType      NodeType
}

func (t telemetryFilterConfig) MetricsForClass(c networking.ListenerClass) metricConfig {
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

// computedTelemetries contains the various Telemetry configurations in scope for a given proxy.
// This can include the root namespace, namespace, and workload Telemetries combined
type computedTelemetries struct {
	telemetryKey
	Metrics []*tpb.Metrics
	Logging []*computedAccessLogging
	Tracing []*tpb.Tracing
}

// computedAccessLogging contains the various AccessLogging configurations in scope for a given proxy,
// include combined configurations for one of the following levels: 1. the root namespace level
// 2. namespace level 3. workload level combined.
type computedAccessLogging struct {
	telemetryKey
	Logging []*tpb.AccessLogging
}

type TracingConfig struct {
	ServerSpec TracingSpec
	ClientSpec TracingSpec
}

type TracingSpec struct {
	Provider                     *meshconfig.MeshConfig_ExtensionProvider
	Disabled                     bool
	RandomSamplingPercentage     *float64
	CustomTags                   map[string]*tpb.Tracing_CustomTag
	UseRequestIDForTraceSampling bool
	EnableIstioTags              bool
}

type LoggingConfig struct {
	Disabled  bool
	AccessLog *accesslog.AccessLog
	Provider  *meshconfig.MeshConfig_ExtensionProvider
	Filter    *tpb.AccessLogging_Filter
}

type loggingSpec struct {
	Disabled bool
	Filter   *tpb.AccessLogging_Filter
}

func workloadMode(class networking.ListenerClass) tpb.WorkloadMode {
	switch class {
	case networking.ListenerClassGateway:
		return tpb.WorkloadMode_CLIENT
	case networking.ListenerClassSidecarInbound:
		return tpb.WorkloadMode_SERVER
	case networking.ListenerClassSidecarOutbound:
		return tpb.WorkloadMode_CLIENT
	case networking.ListenerClassUndefined:
		// this should not happen, just in case
		return tpb.WorkloadMode_CLIENT
	}

	return tpb.WorkloadMode_CLIENT
}

// AccessLogging returns the logging configuration for a given proxy and listener class.
// If nil or empty configuration is returned, access logs are not configured via Telemetry and should use fallback mechanisms.
// If access logging is explicitly disabled, a configuration with disabled set to true is returned.
func (t *Telemetries) AccessLogging(push *PushContext, proxy *Proxy, class networking.ListenerClass, svc *Service) []LoggingConfig {
	ct := t.applicableTelemetries(proxy, svc)
	if len(ct.Logging) == 0 && len(t.meshConfig.GetDefaultProviders().GetAccessLogging()) == 0 {
		// No Telemetry API configured, fall back to legacy mesh config setting
		return nil
	}

	key := loggingKey{
		telemetryKey: ct.telemetryKey,
		Class:        class,
		Version:      proxy.GetIstioVersion(),
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	precomputed, ok := t.computedLoggingConfig[key]
	if ok {
		return precomputed
	}

	providers := mergeLogs(ct.Logging, t.meshConfig, workloadMode(class))
	cfgs := make([]LoggingConfig, 0, len(providers))
	for p, v := range providers {
		fp := t.fetchProvider(p)
		if fp == nil {
			log.Debugf("fail to fetch provider %s", p)
			continue
		}
		cfg := LoggingConfig{
			Provider: fp,
			Filter:   v.Filter,
			Disabled: v.Disabled,
		}

		al := telemetryAccessLog(push, proxy, fp)
		if al == nil {
			// stackdriver will be handled in HTTPFilters/TCPFilters
			continue
		}
		cfg.AccessLog = al
		cfgs = append(cfgs, cfg)
	}

	// Sort the access logs by provider name for deterministic ordering
	sort.Slice(cfgs, func(i, j int) bool {
		return cfgs[i].Provider.Name < cfgs[j].Provider.Name
	})

	t.computedLoggingConfig[key] = cfgs
	return cfgs
}

// Tracing returns the logging tracing for a given proxy. If nil is returned, tracing
// are not configured via Telemetry and should use fallback mechanisms. If a non-nil but disabled is set,
// then tracing is explicitly disabled.
// A service can optionally be provided to include service-attached Telemetry config.
func (t *Telemetries) Tracing(proxy *Proxy, svc *Service) *TracingConfig {
	ct := t.applicableTelemetries(proxy, svc)

	providerNames := t.meshConfig.GetDefaultProviders().GetTracing()
	hasDefaultProvider := len(providerNames) > 0

	if len(ct.Tracing) == 0 && !hasDefaultProvider {
		return nil
	}

	clientSpec := TracingSpec{UseRequestIDForTraceSampling: true, EnableIstioTags: true}
	serverSpec := TracingSpec{UseRequestIDForTraceSampling: true, EnableIstioTags: true}

	if hasDefaultProvider {
		// todo: what do we want to do with more than one default provider?
		// for now, use only the first provider.
		fetched := t.fetchProvider(providerNames[0])
		clientSpec.Provider = fetched
		serverSpec.Provider = fetched
	}

	for _, m := range ct.Tracing {
		names := getProviderNames(m.Providers)

		specs := []*TracingSpec{&clientSpec, &serverSpec}
		if m.Match != nil {
			switch m.Match.Mode {
			case tpb.WorkloadMode_CLIENT:
				specs = []*TracingSpec{&clientSpec}
			case tpb.WorkloadMode_SERVER:
				specs = []*TracingSpec{&serverSpec}
			}
		}

		if len(names) > 0 {
			// NOTE: we only support a single provider per mode
			// so, choosing the first provider returned in the list
			// is the "safest"
			fetched := t.fetchProvider(names[0])
			for _, spec := range specs {
				spec.Provider = fetched
			}
		}

		// Now merge in any overrides
		if m.DisableSpanReporting != nil {
			for _, spec := range specs {
				spec.Disabled = m.DisableSpanReporting.GetValue()
			}
		}
		// TODO: metrics overrides do a deep merge, but here we do a shallow merge.
		// We should consider if we want to reconcile the two.
		if m.CustomTags != nil {
			for _, spec := range specs {
				spec.CustomTags = m.CustomTags
			}
		}
		if m.RandomSamplingPercentage != nil {
			for _, spec := range specs {
				spec.RandomSamplingPercentage = ptr.Of(m.RandomSamplingPercentage.GetValue())
			}
		}
		if m.UseRequestIdForTraceSampling != nil {
			for _, spec := range specs {
				spec.UseRequestIDForTraceSampling = m.UseRequestIdForTraceSampling.Value
			}
		}
		if m.EnableIstioTags != nil {
			for _, spec := range specs {
				spec.EnableIstioTags = m.EnableIstioTags.Value
			}
		}
	}

	// If no provider is configured (and retrieved) for the tracing specs,
	// then we will disable the configuration.
	if clientSpec.Provider == nil {
		clientSpec.Disabled = true
	}
	if serverSpec.Provider == nil {
		serverSpec.Disabled = true
	}

	cfg := TracingConfig{
		ClientSpec: clientSpec,
		ServerSpec: serverSpec,
	}
	return &cfg
}

// HTTPFilters computes the HttpFilter for a given proxy/class
func (t *Telemetries) HTTPFilters(proxy *Proxy, class networking.ListenerClass, svc *Service) []*hcm.HttpFilter {
	if res := t.telemetryFilters(proxy, class, networking.ListenerProtocolHTTP, svc); res != nil {
		return res.([]*hcm.HttpFilter)
	}
	return nil
}

// TCPFilters computes the TCPFilters for a given proxy/class
func (t *Telemetries) TCPFilters(proxy *Proxy, class networking.ListenerClass, svc *Service) []*listener.Filter {
	if res := t.telemetryFilters(proxy, class, networking.ListenerProtocolTCP, svc); res != nil {
		return res.([]*listener.Filter)
	}
	return nil
}

// applicableTelemetries fetches the relevant telemetry configurations for a given proxy
func (t *Telemetries) applicableTelemetries(proxy *Proxy, svc *Service) computedTelemetries {
	if t == nil {
		return computedTelemetries{}
	}

	namespace := proxy.ConfigNamespace
	// Order here matters. The latter elements will override the first elements
	ms := []*tpb.Metrics{}
	ls := []*computedAccessLogging{}
	ts := []*tpb.Tracing{}
	key := telemetryKey{}
	if t.RootNamespace != "" {
		telemetry := t.namespaceWideTelemetryConfig(t.RootNamespace)
		if telemetry != (Telemetry{}) {
			key.Root = types.NamespacedName{Name: telemetry.Name, Namespace: telemetry.Namespace}
			ms = append(ms, telemetry.Spec.GetMetrics()...)
			if len(telemetry.Spec.GetAccessLogging()) != 0 {
				ls = append(ls, &computedAccessLogging{
					telemetryKey: telemetryKey{
						Root: key.Root,
					},
					Logging: telemetry.Spec.GetAccessLogging(),
				})
			}
			ts = append(ts, telemetry.Spec.GetTracing()...)
		}
	}

	if namespace != t.RootNamespace {
		telemetry := t.namespaceWideTelemetryConfig(namespace)
		if telemetry != (Telemetry{}) {
			key.Namespace = types.NamespacedName{Name: telemetry.Name, Namespace: telemetry.Namespace}
			ms = append(ms, telemetry.Spec.GetMetrics()...)
			if len(telemetry.Spec.GetAccessLogging()) != 0 {
				ls = append(ls, &computedAccessLogging{
					telemetryKey: telemetryKey{
						Namespace: key.Namespace,
					},
					Logging: telemetry.Spec.GetAccessLogging(),
				})
			}
			ts = append(ts, telemetry.Spec.GetTracing()...)
		}
	}

	ct := &computedTelemetries{
		telemetryKey: key,
		Metrics:      ms,
		Logging:      ls,
		Tracing:      ts,
	}

	matcher := PolicyMatcherForProxy(proxy).WithService(svc).WithRootNamespace(t.RootNamespace)
	for _, telemetry := range t.NamespaceToTelemetries[namespace] {
		spec := telemetry.Spec
		// Namespace wide policy; already handled above
		if len(spec.GetSelector().GetMatchLabels()) == 0 && len(GetTargetRefs(spec)) == 0 {
			continue
		}
		if matcher.ShouldAttachPolicy(gvk.Telemetry, telemetry.NamespacedName(), spec) {
			ct = appendApplicableTelemetries(ct, telemetry, spec)
		} else {
			log.Debug("There isn't a match between the workload and the policy. Policy is ignored.")
		}
	}

	return *ct
}

func appendApplicableTelemetries(ct *computedTelemetries, tel Telemetry, spec *tpb.Telemetry) *computedTelemetries {
	ct.telemetryKey.Workload = types.NamespacedName{Name: tel.Name, Namespace: tel.Namespace}
	ct.Metrics = append(ct.Metrics, spec.GetMetrics()...)
	if len(tel.Spec.GetAccessLogging()) != 0 {
		ct.Logging = append(ct.Logging, &computedAccessLogging{
			telemetryKey: telemetryKey{
				Workload: types.NamespacedName{Name: tel.Name, Namespace: tel.Namespace},
			},
			Logging: tel.Spec.GetAccessLogging(),
		})
	}
	ct.Tracing = append(ct.Tracing, spec.GetTracing()...)

	return ct
}

// telemetryFilters computes the filters for the given proxy/class and protocol. This computes the
// set of applicable Telemetries, merges them, then translates to the appropriate filters based on the
// extension providers in the mesh config. Where possible, the result is cached.
// Currently, this includes metrics and access logging, as some providers are implemented in filters.
func (t *Telemetries) telemetryFilters(proxy *Proxy, class networking.ListenerClass, protocol networking.ListenerProtocol, svc *Service) any {
	if t == nil {
		return nil
	}

	c := t.applicableTelemetries(proxy, svc)

	key := metricsKey{
		telemetryKey: c.telemetryKey,
		Class:        class,
		Protocol:     protocol,
		ProxyType:    proxy.Type,
	}
	if svc != nil {
		key.Service = types.NamespacedName{Name: svc.Attributes.Name, Namespace: svc.Attributes.Namespace}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	precomputed, f := t.computedMetricsFilters[key]
	if f {
		return precomputed
	}

	// First, take all the metrics configs and transform them into a normalized form
	tmm := mergeMetrics(c.Metrics, t.meshConfig)
	log.Debugf("merged metrics, proxyID: %s metrics: %+v", proxy.ID, tmm)
	// Additionally, fetch relevant access logging configurations
	tml := mergeLogs(c.Logging, t.meshConfig, workloadMode(class))

	// The above result is in a nested map to deduplicate responses. This loses ordering, so we convert to
	// a list to retain stable naming
	allKeys := sets.New[string]()
	for k, v := range tml {
		if v.Disabled {
			continue
		}
		allKeys.Insert(k)
	}
	for k := range tmm {
		allKeys.Insert(k)
	}

	rotationInterval := getInterval(features.MetricRotationInterval, defaultMetricRotationInterval)
	gracefulDeletionInterval := getInterval(features.MetricGracefulDeletionInterval, defaultMetricGracefulDeletionInterval)

	m := make([]telemetryFilterConfig, 0, allKeys.Len())
	for _, k := range sets.SortedList(allKeys) {
		p := t.fetchProvider(k)
		if p == nil {
			continue
		}
		loggingCfg, logging := tml[k]
		mertricCfg, metrics := tmm[k]

		mertricCfg.RotationInterval = rotationInterval
		mertricCfg.GracefulDeletionInterval = gracefulDeletionInterval

		cfg := telemetryFilterConfig{
			Provider:      p,
			metricsConfig: mertricCfg,
			AccessLogging: logging && !loggingCfg.Disabled,
			Metrics:       metrics,
			LogsFilter:    tml[p.Name].Filter,
			NodeType:      proxy.Type,
		}
		m = append(m, cfg)
	}

	var res any
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

// default value for metric rotation interval and graceful deletion interval,
// more details can be found in here: https://github.com/istio/proxy/blob/master/source/extensions/filters/http/istio_stats/config.proto#L116
var (
	defaultMetricRotationInterval         = 0 * time.Second
	defaultMetricGracefulDeletionInterval = 5 * time.Minute
)

// getInterval return nil to reduce the size of the config, when equal to the default.
func getInterval(input, defaultValue time.Duration) *durationpb.Duration {
	if input == defaultValue {
		return nil
	}

	return durationpb.New(input)
}

// mergeLogs returns the set of providers for the given logging configuration.
// The provider names are mapped to any applicable access logging filter that has been applied in provider configuration.
func mergeLogs(logs []*computedAccessLogging, mesh *meshconfig.MeshConfig, mode tpb.WorkloadMode) map[string]loggingSpec {
	providers := map[string]loggingSpec{}

	if len(logs) == 0 {
		for _, dp := range mesh.GetDefaultProviders().GetAccessLogging() {
			// Insert the default provider.
			providers[dp] = loggingSpec{}
		}
		return providers
	}
	providerNames := mesh.GetDefaultProviders().GetAccessLogging()
	filters := map[string]loggingSpec{}
	for _, m := range logs {
		names := sets.New[string]()
		for _, p := range m.Logging {
			if !matchWorkloadMode(p.Match, mode) {
				continue
			}
			subProviders := getProviderNames(p.Providers)
			names.InsertAll(subProviders...)

			for _, prov := range subProviders {
				filters[prov] = loggingSpec{
					Filter: p.Filter,
				}
			}
		}

		if names.Len() > 0 {
			providerNames = names.UnsortedList()
		}
	}
	inScopeProviders := sets.New(providerNames...)

	parentProviders := mesh.GetDefaultProviders().GetAccessLogging()
	for _, l := range logs {
		for _, m := range l.Logging {
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

				if !matchWorkloadMode(m.Match, mode) {
					continue
				}

				// see UT: server - multi filters disabled
				if m.GetDisabled().GetValue() {
					providers[provider] = loggingSpec{Disabled: true}
					continue
				}

				providers[provider] = filters[provider]
			}
		}
	}

	return providers
}

func matchWorkloadMode(selector *tpb.AccessLogging_LogSelector, mode tpb.WorkloadMode) bool {
	if selector == nil {
		return true
	}

	if selector.Mode == tpb.WorkloadMode_CLIENT_AND_SERVER {
		return true
	}

	return selector.Mode == mode
}

func (t *Telemetries) namespaceWideTelemetryConfig(namespace string) Telemetry {
	for _, tel := range t.NamespaceToTelemetries[namespace] {
		if len(tel.Spec.GetSelector().GetMatchLabels()) == 0 && len(GetTargetRefs(tel.Spec)) == 0 {
			return tel
		}
	}
	return Telemetry{}
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

func (t *Telemetries) Debug(proxy *Proxy) any {
	// TODO we could use service targets + ambient index to include service-attached here
	at := t.applicableTelemetries(proxy, nil)
	return at
}

var allMetrics = func() []string {
	r := make([]string, 0, len(tpb.MetricSelector_IstioMetric_value))
	for k := range tpb.MetricSelector_IstioMetric_value {
		if k != tpb.MetricSelector_IstioMetric_name[int32(tpb.MetricSelector_ALL_METRICS)] {
			r = append(r, k)
		}
	}
	sort.Strings(r)
	return r
}()

// mergeMetrics merges many Metrics objects into a normalized configuration
func mergeMetrics(metrics []*tpb.Metrics, mesh *meshconfig.MeshConfig) map[string]metricsConfig {
	type metricOverride struct {
		Disabled     *wrappers.BoolValue
		TagOverrides map[string]*tpb.MetricsOverrides_TagOverride
	}
	// provider -> mode -> metric -> overrides
	providers := map[string]map[tpb.WorkloadMode]map[string]metricOverride{}

	if len(metrics) == 0 {
		for _, dp := range mesh.GetDefaultProviders().GetMetrics() {
			// Insert the default provider. It has no overrides; presence of the key is sufficient to
			// get the filter created.
			providers[dp] = map[tpb.WorkloadMode]map[string]metricOverride{}
		}
	}

	providerNames := mesh.GetDefaultProviders().GetMetrics()
	for _, m := range metrics {
		names := getProviderNames(m.Providers)
		// If providers is set, it overrides the parent. If not, inherent from the parent. It is not a deep merge.
		if len(names) > 0 {
			providerNames = names
		}
	}
	// Record the names of all providers we should configure. Anything else we will ignore
	inScopeProviders := sets.New(providerNames...)

	parentProviders := mesh.GetDefaultProviders().GetMetrics()
	disabledAllMetricsProviders := sets.New[string]()
	reportingIntervals := map[string]*durationpb.Duration{}
	for _, m := range metrics {
		providerNames := getProviderNames(m.Providers)
		// If providers is not set, use parent's
		if len(providerNames) == 0 {
			providerNames = parentProviders
		}

		reportInterval := m.GetReportingInterval()
		parentProviders = providerNames
		for _, provider := range providerNames {
			if !inScopeProviders.Contains(provider) {
				// We don't care about this, remove it
				// This occurs when a top level provider is later disabled by a lower level
				continue
			}

			if reportInterval != nil {
				reportingIntervals[provider] = reportInterval
			}

			if _, f := providers[provider]; !f {
				providers[provider] = map[tpb.WorkloadMode]map[string]metricOverride{
					tpb.WorkloadMode_CLIENT: {},
					tpb.WorkloadMode_SERVER: {},
				}
			}

			mp := providers[provider]
			// For each override, we normalize the configuration. The metrics list is an ordered list - latter
			// elements have precedence. As a result, we will apply updates on top of previous entries.
			for _, o := range m.Overrides {
				// if we disable all metrics, we should drop the entire filter
				if isAllMetrics(o.GetMatch()) && o.Disabled.GetValue() {
					for _, mode := range getModes(o.GetMatch().GetMode()) {
						key := metricProviderModeKey(provider, mode)
						disabledAllMetricsProviders.Insert(key)
					}

					continue
				}

				metricsNames := getMatches(o.GetMatch())
				// If client or server is set explicitly, only apply there. Otherwise, we will apply to both.
				// Note: client and server keys may end up the same, which is fine
				for _, mode := range getModes(o.GetMatch().GetMode()) {
					// root namespace disables all, but then enables them by namespace scoped
					key := metricProviderModeKey(provider, mode)
					disabledAllMetricsProviders.Delete(key)
					// Next, get all matches.
					// This is a bit funky because the matches are oneof of ENUM and customer metric. We normalize
					// these to strings, so we may end up with a list like [REQUEST_COUNT, my-customer-metric].
					// TODO: we always flatten ALL_METRICS into each metric mode. For some stats providers (prometheus),
					// we are able to apply overrides to all metrics directly rather than duplicating the config.
					// We should tweak this to collapse to this mode where possible
					for _, metricName := range metricsNames {
						if _, f := mp[mode]; !f {
							mp[mode] = map[string]metricOverride{}
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

	processed := map[string]metricsConfig{}
	for provider, modeMap := range providers {
		tmm := processed[provider]
		tmm.ReportingInterval = reportingIntervals[provider]

		for mode, metricMap := range modeMap {
			key := metricProviderModeKey(provider, mode)
			if disabledAllMetricsProviders.Contains(key) {
				switch mode {
				case tpb.WorkloadMode_CLIENT:
					tmm.ClientMetrics.Disabled = true
				case tpb.WorkloadMode_SERVER:
					tmm.ServerMetrics.Disabled = true
				}
				continue
			}

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

				switch mode {
				case tpb.WorkloadMode_CLIENT:
					tmm.ClientMetrics.Overrides = append(tmm.ClientMetrics.Overrides, mo)
				default:
					tmm.ServerMetrics.Overrides = append(tmm.ServerMetrics.Overrides, mo)
				}
			}
		}

		// Keep order deterministic
		sort.Slice(tmm.ServerMetrics.Overrides, func(i, j int) bool {
			return tmm.ServerMetrics.Overrides[i].Name < tmm.ServerMetrics.Overrides[j].Name
		})
		sort.Slice(tmm.ClientMetrics.Overrides, func(i, j int) bool {
			return tmm.ClientMetrics.Overrides[i].Name < tmm.ClientMetrics.Overrides[j].Name
		})
		processed[provider] = tmm
	}
	return processed
}

func metricProviderModeKey(provider string, mode tpb.WorkloadMode) string {
	return fmt.Sprintf("%s/%s", provider, mode)
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

func isAllMetrics(match *tpb.MetricSelector) bool {
	switch m := match.GetMetricMatch().(type) {
	case *tpb.MetricSelector_CustomMetric:
		return false
	case *tpb.MetricSelector_Metric:
		return m.Metric == tpb.MetricSelector_ALL_METRICS
	default:
		return true
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

// telemetryFilterHandled contains the number of providers we handle below.
// This is to ensure this stays in sync as new handlers are added
// STOP. DO NOT UPDATE THIS WITHOUT UPDATING buildHTTPTelemetryFilter and buildTCPTelemetryFilter.
const telemetryFilterHandled = 14

func buildHTTPTelemetryFilter(class networking.ListenerClass, metricsCfg []telemetryFilterConfig) []*hcm.HttpFilter {
	res := make([]*hcm.HttpFilter, 0, len(metricsCfg))
	for _, cfg := range metricsCfg {
		switch cfg.Provider.GetProvider().(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_Prometheus:
			if statsCfg := generateStatsConfig(class, cfg, cfg.NodeType == Waypoint); statsCfg != nil {
				f := &hcm.HttpFilter{
					Name:       xds.StatsFilterName,
					ConfigType: &hcm.HttpFilter_TypedConfig{TypedConfig: statsCfg},
				}
				res = append(res, f)
			}
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
			if cfg := generateStatsConfig(class, telemetryCfg, telemetryCfg.NodeType == Waypoint); cfg != nil {
				f := &listener.Filter{
					Name:       xds.StatsFilterName,
					ConfigType: &listener.Filter_TypedConfig{TypedConfig: cfg},
				}
				res = append(res, f)
			}
		default:
			// Only prometheus  supported currently
			continue
		}
	}
	return res
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

func generateStatsConfig(class networking.ListenerClass, filterConfig telemetryFilterConfig, isWaypoint bool) *anypb.Any {
	if !filterConfig.Metrics {
		// No metric for prometheus
		return nil
	}

	listenerCfg := filterConfig.MetricsForClass(class)
	if listenerCfg.Disabled {
		// no metrics for this listener
		return nil
	}

	cfg := stats.PluginConfig{
		DisableHostHeaderFallback: disableHostHeaderFallback(class),
		TcpReportingDuration:      filterConfig.ReportingInterval,
		RotationInterval:          filterConfig.RotationInterval,
		GracefulDeletionInterval:  filterConfig.GracefulDeletionInterval,
	}
	if isWaypoint {
		cfg.Reporter = stats.Reporter_SERVER_GATEWAY
	}

	for _, override := range listenerCfg.Overrides {
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

	return protoconv.MessageToAny(&cfg)
}

func disableHostHeaderFallback(class networking.ListenerClass) bool {
	return class == networking.ListenerClassSidecarInbound || class == networking.ListenerClassGateway
}

// Equal compares two computedTelemetries for equality. This was created to help with testing. Because of the nature of the structs being compared,
// it is safer to use cmp.Equal as opposed to reflect.DeepEqual. Also, because of the way the structs are generated, it is not possible to use
// cmpopts.IgnoreUnexported without risking flakiness if those third party types that are relied on change. Next best thing is to use a custom
// comparer as defined below. When cmp.Equal is called on this type, this will be leveraged by cmp.Equal to do the comparison see
// https://godoc.org/github.com/google/go-cmp/cmp#Equal for more info.
func (ct *computedTelemetries) Equal(other *computedTelemetries) bool {
	if ct == nil && other == nil {
		return true
	}
	if ct != nil && other == nil || ct == nil && other != nil {
		return false
	}
	if len(ct.Metrics) != len(other.Metrics) || len(ct.Logging) != len(other.Logging) || len(ct.Tracing) != len(other.Tracing) {
		return false
	}
	// Sort each slice so that we can compare them in order. Comparison is on the fields that are used in the test cases.
	sort.SliceStable(ct.Metrics, func(i, j int) bool {
		return ct.Metrics[i].Providers[0].Name < ct.Metrics[j].Providers[0].Name
	})
	sort.SliceStable(other.Metrics, func(i, j int) bool {
		return other.Metrics[i].Providers[0].Name < other.Metrics[j].Providers[0].Name
	})
	for i := range ct.Metrics {
		if ct.Metrics[i].ReportingInterval != nil && other.Metrics[i].ReportingInterval != nil {
			if ct.Metrics[i].ReportingInterval.AsDuration() != other.Metrics[i].ReportingInterval.AsDuration() {
				return false
			}
		}
		if ct.Metrics[i].Providers != nil && other.Metrics[i].Providers != nil {
			if ct.Metrics[i].Providers[0].Name != other.Metrics[i].Providers[0].Name {
				return false
			}
		}
	}
	sort.SliceStable(ct.Logging, func(i, j int) bool {
		return ct.Logging[i].telemetryKey.Root.Name < ct.Logging[j].telemetryKey.Root.Name
	})
	sort.SliceStable(other.Logging, func(i, j int) bool {
		return other.Logging[i].telemetryKey.Root.Name < other.Logging[j].telemetryKey.Root.Name
	})
	for i := range ct.Logging {
		if ct.Logging[i].telemetryKey != other.Logging[i].telemetryKey {
			return false
		}
		if ct.Logging[i].Logging != nil && other.Logging[i].Logging != nil {
			if ct.Logging[i].Logging[0].Providers[0].Name != other.Logging[i].Logging[0].Providers[0].Name {
				return false
			}
		}
	}
	sort.SliceStable(ct.Tracing, func(i, j int) bool {
		return ct.Tracing[i].Providers[0].Name < ct.Tracing[j].Providers[0].Name
	})
	sort.SliceStable(other.Tracing, func(i, j int) bool {
		return other.Tracing[i].Providers[0].Name < other.Tracing[j].Providers[0].Name
	})
	for i := range ct.Tracing {
		if ct.Tracing[i].Match != nil && other.Tracing[i].Match != nil {
			if ct.Tracing[i].Match.Mode != other.Tracing[i].Match.Mode {
				return false
			}
		}
		if ct.Tracing[i].Providers != nil && other.Tracing[i].Providers != nil {
			if ct.Tracing[i].Providers[0].Name != other.Tracing[i].Providers[0].Name {
				return false
			}
		}
	}
	return true
}
