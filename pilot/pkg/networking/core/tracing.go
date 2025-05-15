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

package core

import (
	"fmt"
	"net/url"
	"sort"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tracingcfg "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	otelsamplers "github.com/envoyproxy/go-control-plane/envoy/extensions/tracers/opentelemetry/samplers/v3"
	envoy_type_metadata_v3 "github.com/envoyproxy/go-control-plane/envoy/type/metadata/v3"
	tracing "github.com/envoyproxy/go-control-plane/envoy/type/tracing/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	telemetrypb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/pkg/xds/requestidextension"
	"istio.io/istio/pkg/env"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/wellknown"
)

const (
	envoyDatadog       = "envoy.tracers.datadog"
	envoyOpenTelemetry = "envoy.tracers.opentelemetry"
	envoySkywalking    = "envoy.tracers.skywalking"
	envoyZipkin        = "envoy.tracers.zipkin"
)

// this is used for testing. it should not be changed in regular code.
var clusterLookupFn = model.LookupCluster

type typedConfigGenFn func() (*anypb.Any, error)

func configureTracing(
	push *model.PushContext,
	proxy *model.Proxy,
	httpConnMgr *hcm.HttpConnectionManager,
	class networking.ListenerClass,
	svc *model.Service,
) *requestidextension.UUIDRequestIDExtensionContext {
	tracingCfg := push.Telemetry.Tracing(proxy, svc)
	return configureTracingFromTelemetry(tracingCfg, push, proxy, httpConnMgr, class)
}

func configureTracingFromTelemetry(
	tracing *model.TracingConfig,
	push *model.PushContext,
	proxy *model.Proxy,
	h *hcm.HttpConnectionManager,
	class networking.ListenerClass,
) *requestidextension.UUIDRequestIDExtensionContext {
	proxyCfg := proxy.Metadata.ProxyConfigOrDefault(push.Mesh.DefaultConfig)
	// If there is no telemetry config defined, fallback to legacy mesh config.
	if tracing == nil {
		meshCfg := push.Mesh
		if !meshCfg.EnableTracing {
			log.Debug("No valid tracing configuration found")
			return nil
		}
		// use the prior configuration bits of sampling and custom tags
		h.Tracing = &hcm.HttpConnectionManager_Tracing{}
		configureSampling(h.Tracing, proxyConfigSamplingValue(proxyCfg))
		configureCustomTags(nil, h.Tracing, map[string]*telemetrypb.Tracing_CustomTag{}, proxyCfg, proxy)
		if proxyCfg.GetTracing().GetMaxPathTagLength() != 0 {
			h.Tracing.MaxPathTagLength = wrapperspb.UInt32(proxyCfg.GetTracing().MaxPathTagLength)
		}
		return nil
	}
	spec := tracing.ServerSpec
	if class == networking.ListenerClassSidecarOutbound || class == networking.ListenerClassGateway {
		spec = tracing.ClientSpec
	}

	if spec.Disabled {
		return nil
	}

	var useCustomSampler bool
	if spec.Provider != nil {
		hcmTracing, hasCustomSampler, err := configureFromProviderConfig(push, proxy, spec.Provider)
		if err != nil {
			log.Warnf("Not able to configure requested tracing provider %q: %v", spec.Provider.Name, err)
			return nil
		}
		h.Tracing = hcmTracing
		useCustomSampler = hasCustomSampler
	} else {
		// TODO: should this `return nil, nil` instead ?
		log.Warnf("Not able to configure tracing provider. Provider lookup failed.")
		h.Tracing = &hcm.HttpConnectionManager_Tracing{}
		// TODO: transition to configuring providers from proxy config here?
		// something like: configureFromProxyConfig(tracingCfg, opts.proxy.Metadata.ProxyConfig.Tracing)
	}

	// provider.sampler > telemetry.RandomSamplingPercentage > defaultConfig.tracing.sampling > PILOT_TRACE_SAMPLING
	var sampling float64
	if useCustomSampler {
		// If the TracingProvider has a custom sampler (OTel Sampler)
		// the sampling percentage is set to 100% so all spans arrive at the sampler for its decision.
		sampling = 100
	} else if spec.RandomSamplingPercentage != nil {
		sampling = *spec.RandomSamplingPercentage
	} else {
		// gracefully fallback to MeshConfig configuration. It will act as an implicit
		// parent configuration during transition period.
		sampling = proxyConfigSamplingValue(proxyCfg)
	}

	configureSampling(h.Tracing, sampling)
	configureCustomTags(&spec, h.Tracing, spec.CustomTags, proxyCfg, proxy)

	// if there is configured max tag length somewhere, fallback to it.
	if h.GetTracing().GetMaxPathTagLength() == nil && proxyCfg.GetTracing().GetMaxPathTagLength() != 0 {
		h.Tracing.MaxPathTagLength = wrapperspb.UInt32(proxyCfg.GetTracing().MaxPathTagLength)
	}

	reqIDExtension := &requestidextension.UUIDRequestIDExtensionContext{}
	reqIDExtension.UseRequestIDForTraceSampling = spec.UseRequestIDForTraceSampling
	return reqIDExtension
}

// configureFromProviderConfigHandled contains the number of providers we handle below.
// This is to ensure this stays in sync as new handlers are added
// STOP. DO NOT UPDATE THIS WITHOUT UPDATING configureFromProviderConfig.
const configureFromProviderConfigHandled = 14

func configureFromProviderConfig(pushCtx *model.PushContext, proxy *model.Proxy,
	providerCfg *meshconfig.MeshConfig_ExtensionProvider,
) (*hcm.HttpConnectionManager_Tracing, bool, error) {
	startChildSpan := false
	if proxy.Type == model.Router {
		startChildSpan = features.SpawnUpstreamSpanForGateway
	}
	useCustomSampler := false
	var serviceCluster string
	var maxTagLength uint32
	var providerConfig typedConfigGenFn
	var providerName string
	if proxy.XdsNode != nil {
		serviceCluster = proxy.XdsNode.Cluster
	}
	switch provider := providerCfg.Provider.(type) {
	case *meshconfig.MeshConfig_ExtensionProvider_Zipkin:
		maxTagLength = provider.Zipkin.GetMaxTagLength()
		providerName = envoyZipkin
		providerConfig = func() (*anypb.Any, error) {
			hostname, cluster, err := clusterLookupFn(pushCtx, provider.Zipkin.GetService(), int(provider.Zipkin.GetPort()))
			if err != nil {
				model.IncLookupClusterFailures("zipkin")
				return nil, fmt.Errorf("could not find cluster for tracing provider %q: %v", provider, err)
			}
			return zipkinConfig(hostname, cluster, provider.Zipkin.GetPath(), !provider.Zipkin.GetEnable_64BitTraceId())
		}
	case *meshconfig.MeshConfig_ExtensionProvider_Datadog:
		maxTagLength = provider.Datadog.GetMaxTagLength()
		providerName = envoyDatadog
		providerConfig = func() (*anypb.Any, error) {
			hostname, cluster, err := clusterLookupFn(pushCtx, provider.Datadog.GetService(), int(provider.Datadog.GetPort()))
			if err != nil {
				model.IncLookupClusterFailures("datadog")
				return nil, fmt.Errorf("could not find cluster for tracing provider %q: %v", provider, err)
			}
			return datadogConfig(serviceCluster, hostname, cluster)
		}
	case *meshconfig.MeshConfig_ExtensionProvider_Lightstep:
		log.Warnf("Lightstep provider is deprecated, please use OpenTelemetry instead")
	case *meshconfig.MeshConfig_ExtensionProvider_Skywalking:
		maxTagLength = 0
		providerName = envoySkywalking
		providerConfig = func() (*anypb.Any, error) {
			hostname, clusterName, err := clusterLookupFn(pushCtx, provider.Skywalking.GetService(), int(provider.Skywalking.GetPort()))
			if err != nil {
				model.IncLookupClusterFailures("skywalking")
				return nil, fmt.Errorf("could not find cluster for tracing provider %q: %v", provider, err)
			}
			return skywalkingConfig(clusterName, hostname)
		}

		startChildSpan = true
	case *meshconfig.MeshConfig_ExtensionProvider_Opentelemetry:
		maxTagLength = provider.Opentelemetry.GetMaxTagLength()
		providerName = envoyOpenTelemetry
		providerConfig = func() (*anypb.Any, error) {
			tracingCfg, hasCustomSampler, err := otelConfig(serviceCluster, provider.Opentelemetry, pushCtx)
			useCustomSampler = hasCustomSampler
			return tracingCfg, err
		}
		// Providers without any tracing support
		// Explicitly list to be clear what does and does not support tracing
	case *meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp,
		*meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc,
		*meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpAls,
		*meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpAls,
		*meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls,
		*meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog,
		*meshconfig.MeshConfig_ExtensionProvider_Prometheus:
		return nil, false, fmt.Errorf("provider %T does not support tracing", provider)
		// Should never happen, but just in case we forget to add one
	default:
		return nil, false, fmt.Errorf("provider %T does not support tracing", provider)
	}
	hcmTracing, err := buildHCMTracing(providerName, startChildSpan, maxTagLength, providerConfig)
	return hcmTracing, useCustomSampler, err
}

func zipkinConfig(hostname, cluster, endpoint string, enable128BitTraceID bool) (*anypb.Any, error) {
	if endpoint == "" {
		endpoint = "/api/v2/spans" // envoy deprecated v1 support
	}
	zc := &tracingcfg.ZipkinConfig{
		CollectorCluster:         cluster,
		CollectorEndpoint:        endpoint,
		CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON, // use v2 JSON for now
		CollectorHostname:        hostname,                          // http host header
		TraceId_128Bit:           enable128BitTraceID,               // istio default enable 128 bit trace id
		SharedSpanContext:        wrapperspb.Bool(false),
	}
	return protoconv.MessageToAnyWithError(zc)
}

func datadogConfig(serviceName, hostname, cluster string) (*anypb.Any, error) {
	dc := &tracingcfg.DatadogConfig{
		CollectorCluster:  cluster,
		ServiceName:       serviceName,
		CollectorHostname: hostname,
	}
	return protoconv.MessageToAnyWithError(dc)
}

func otelConfig(serviceName string, otelProvider *meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider,
	pushCtx *model.PushContext,
) (*anypb.Any, bool, error) {
	hostname, cluster, err := clusterLookupFn(pushCtx, otelProvider.GetService(), int(otelProvider.GetPort()))
	if err != nil {
		model.IncLookupClusterFailures("opentelemetry")
		return nil, false, fmt.Errorf("could not find cluster for tracing provider %q: %v", otelProvider, err)
	}

	hasCustomSampler := false
	oc := &tracingcfg.OpenTelemetryConfig{
		ServiceName: serviceName,
	}

	if otelProvider.GetHttp() != nil {
		// export via HTTP
		httpService := otelProvider.GetHttp()
		te, err := url.JoinPath(hostname, httpService.GetPath())
		if err != nil {
			return nil, false, fmt.Errorf("could not parse otlp/http traces endpoint: %v", err)
		}
		oc.HttpService = &core.HttpService{
			HttpUri: &core.HttpUri{
				Uri: te,
				HttpUpstreamType: &core.HttpUri_Cluster{
					Cluster: cluster,
				},
				Timeout: httpService.GetTimeout(),
			},
			RequestHeadersToAdd: buildHTTPHeaders(httpService.GetHeaders()),
		}

	} else {
		// export via gRPC
		oc.GrpcService = &core.GrpcService{
			TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
					ClusterName: cluster,
					Authority:   hostname,
				},
			},
			Timeout:         otelProvider.GetGrpc().GetTimeout(),
			InitialMetadata: buildInitialMetadata(otelProvider.GetGrpc().GetInitialMetadata()),
		}
	}

	// Add configured resource detectors
	if otelProvider.ResourceDetectors != nil {
		res := []*core.TypedExtensionConfig{}
		rd := otelProvider.ResourceDetectors

		if rd.Environment != nil {
			res = append(res, xdsfilters.EnvironmentResourceDetector)
		}
		if rd.Dynatrace != nil {
			res = append(res, xdsfilters.DynatraceResourceDetector)
		}
		oc.ResourceDetectors = res
	}

	// Add configured Sampler
	if otelProvider.Sampling != nil {
		switch sampler := otelProvider.Sampling.(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler_:
			dts, err := configureDynatraceSampler(hostname, cluster, otelProvider, sampler, pushCtx)
			if err != nil {
				return nil, false, err
			}
			oc.Sampler = dts
		}

		// If any sampler is configured for the OpenTelemetryTracingProvider
		hasCustomSampler = true
	}

	pb, err := anypb.New(oc)
	return pb, hasCustomSampler, err
}

func skywalkingConfig(clusterName, hostname string) (*anypb.Any, error) {
	s := &tracingcfg.SkyWalkingConfig{
		GrpcService: &core.GrpcService{
			TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
					ClusterName: clusterName,
					Authority:   hostname,
				},
			},
		},
	}

	return protoconv.MessageToAnyWithError(s)
}

func configureDynatraceSampler(hostname, cluster string,
	otelProvider *meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider,
	sampler *meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider_DynatraceSampler_,
	pushCtx *model.PushContext,
) (*core.TypedExtensionConfig, error) {
	dsc := &otelsamplers.DynatraceSamplerConfig{
		Tenant:             sampler.DynatraceSampler.GetTenant(),
		ClusterId:          sampler.DynatraceSampler.GetClusterId(),
		RootSpansPerMinute: sampler.DynatraceSampler.GetRootSpansPerMinute(),
	}

	if sampler.DynatraceSampler.HttpService == nil {
		// The Dynatrace sampler can re-use the same Dynatrace service/cluster/headers
		// as configured for the HTTP Exporter. In this case users
		// can achieve a much smaller/simpler config in Istio.

		// Re-use the Dynatrace API Host from the OTLP HTTP exporter
		otlpHTTPService := otelProvider.GetHttp()
		if otlpHTTPService == nil {
			return nil, fmt.Errorf("dynatrace sampler could not get http settings. considering setting the http_service field on the dynatrace sampler")
		}

		uri, err := url.JoinPath(hostname, "api/v2/samplingConfiguration")
		if err != nil {
			return nil, fmt.Errorf("could not parse dynatrace adaptative sampling endpoint %v", err)
		}

		dsc.HttpService = &core.HttpService{
			HttpUri: &core.HttpUri{
				Uri: uri,
				HttpUpstreamType: &core.HttpUri_Cluster{
					Cluster: cluster,
				},
				Timeout: otlpHTTPService.GetTimeout(),
			},
			// Re-use the headers from the OTLP HTTP Exporter
			RequestHeadersToAdd: buildHTTPHeaders(otlpHTTPService.GetHeaders()),
		}
	} else {
		// Dynatrace customers may want to export to a OTel collector
		// but still have the benefits of the Dynatrace Sampler.
		// In this case, the sampler has its own HTTP configuration
		dtapi := sampler.DynatraceSampler.HttpService

		// use Dynatrace API to configure the sampler
		dtHost, dtCluster, err := clusterLookupFn(pushCtx, dtapi.GetService(), int(dtapi.GetPort()))
		if err != nil {
			model.IncLookupClusterFailures("dynatrace")
			return nil, fmt.Errorf("could not find cluster for dynatrace sampler %v", err)
		}

		uri, err := url.JoinPath(dtHost, dtapi.GetHttp().Path)
		if err != nil {
			return nil, fmt.Errorf("could not parse dynatrace adaptative sampling endpoint %v", err)
		}

		dsc.HttpService = &core.HttpService{
			HttpUri: &core.HttpUri{
				Uri: uri,
				HttpUpstreamType: &core.HttpUri_Cluster{
					Cluster: dtCluster,
				},
				Timeout: dtapi.Http.GetTimeout(),
			},
			RequestHeadersToAdd: buildHTTPHeaders(dtapi.Http.GetHeaders()),
		}
	}

	return &core.TypedExtensionConfig{
		Name:        "envoy.tracers.opentelemetry.samplers.dynatrace",
		TypedConfig: protoconv.MessageToAny(dsc),
	}, nil
}

func buildHCMTracing(provider string, startChildSpan bool, maxTagLen uint32, anyFn typedConfigGenFn) (*hcm.HttpConnectionManager_Tracing, error) {
	config := &hcm.HttpConnectionManager_Tracing{}
	if startChildSpan {
		config.SpawnUpstreamSpan = wrapperspb.Bool(startChildSpan)
	}
	cfg, err := anyFn()
	if err != nil {
		return config, fmt.Errorf("could not configure tracing provider %q: %v", provider, err)
	}

	config.Provider = &tracingcfg.Tracing_Http{
		Name:       provider,
		ConfigType: &tracingcfg.Tracing_Http_TypedConfig{TypedConfig: cfg},
	}

	if maxTagLen != 0 {
		config.MaxPathTagLength = &wrapperspb.UInt32Value{Value: maxTagLen}
	}
	return config, nil
}

func dryRunPolicyTraceTag(name, key string) *tracing.CustomTag {
	// The tag will not be populated when not used as there is no default value set for the tag.
	// See https://www.envoyproxy.io/docs/envoy/v1.17.1/configuration/http/http_filters/rbac_filter#dynamic-metadata.
	return &tracing.CustomTag{
		Tag: name,
		Type: &tracing.CustomTag_Metadata_{
			Metadata: &tracing.CustomTag_Metadata{
				Kind: &envoy_type_metadata_v3.MetadataKind{
					Kind: &envoy_type_metadata_v3.MetadataKind_Request_{
						Request: &envoy_type_metadata_v3.MetadataKind_Request{},
					},
				},
				MetadataKey: &envoy_type_metadata_v3.MetadataKey{
					Key: wellknown.HTTPRoleBasedAccessControl,
					Path: []*envoy_type_metadata_v3.MetadataKey_PathSegment{
						{
							Segment: &envoy_type_metadata_v3.MetadataKey_PathSegment_Key{
								Key: key,
							},
						},
					},
				},
			},
		},
	}
}

var optionalPolicyTags = []*tracing.CustomTag{
	dryRunPolicyTraceTag("istio.authorization.dry_run.allow_policy.name", authz_model.RBACShadowRulesAllowStatPrefix+authz_model.RBACShadowEffectivePolicyID),
	dryRunPolicyTraceTag("istio.authorization.dry_run.allow_policy.result", authz_model.RBACShadowRulesAllowStatPrefix+authz_model.RBACShadowEngineResult),
	dryRunPolicyTraceTag("istio.authorization.dry_run.deny_policy.name", authz_model.RBACShadowRulesDenyStatPrefix+authz_model.RBACShadowEffectivePolicyID),
	dryRunPolicyTraceTag("istio.authorization.dry_run.deny_policy.result", authz_model.RBACShadowRulesDenyStatPrefix+authz_model.RBACShadowEngineResult),
}

func buildServiceTags(metadata *model.NodeMetadata, labels map[string]string) []*tracing.CustomTag {
	var revision, service string
	if labels != nil {
		revision = labels["service.istio.io/canonical-revision"]
		service = labels["service.istio.io/canonical-name"]
	}
	if revision == "" {
		revision = "latest"
	}
	// TODO: This should have been properly handled with the injector.
	if service == "" {
		service = "unknown"
	}
	meshID := metadata.MeshID
	if meshID == "" {
		meshID = "unknown"
	}
	namespace := metadata.Namespace
	if namespace == "" {
		namespace = "default"
	}
	clusterID := string(metadata.ClusterID)
	if clusterID == "" {
		clusterID = "unknown"
	}
	return []*tracing.CustomTag{
		{
			Tag: "istio.canonical_revision",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: revision,
				},
			},
		},
		{
			Tag: "istio.canonical_service",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: service,
				},
			},
		},
		{
			Tag: "istio.mesh_id",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: meshID,
				},
			},
		},
		{
			Tag: "istio.namespace",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: namespace,
				},
			},
		},
		{
			Tag: "istio.cluster_id",
			Type: &tracing.CustomTag_Literal_{
				Literal: &tracing.CustomTag_Literal{
					Value: clusterID,
				},
			},
		},
	}
}

func configureSampling(hcmTracing *hcm.HttpConnectionManager_Tracing, providerPercentage float64) {
	hcmTracing.ClientSampling = &xdstype.Percent{
		Value: 100.0,
	}
	hcmTracing.OverallSampling = &xdstype.Percent{
		Value: 100.0,
	}
	hcmTracing.RandomSampling = &xdstype.Percent{
		Value: providerPercentage,
	}
}

func proxyConfigSamplingValue(config *meshconfig.ProxyConfig) float64 {
	// PILOT_TRACE_SAMPLING
	sampling := features.TraceSampling

	// Tracing from default_config
	if config.Tracing != nil && config.Tracing.Sampling != 0.0 {
		sampling = config.Tracing.Sampling

		if sampling > 100.0 {
			sampling = 1.0
		}
	}
	return sampling
}

func configureCustomTags(spec *model.TracingSpec, hcmTracing *hcm.HttpConnectionManager_Tracing,
	providerTags map[string]*telemetrypb.Tracing_CustomTag, proxyCfg *meshconfig.ProxyConfig, node *model.Proxy,
) {
	var tags []*tracing.CustomTag
	if spec == nil {
		// Fallback to legacy mesh config.
		enableIstioTags := true
		if proxyCfg.GetTracing().GetEnableIstioTags() != nil {
			enableIstioTags = proxyCfg.GetTracing().GetEnableIstioTags().GetValue()
		}
		if enableIstioTags {
			tags = append(buildServiceTags(node.Metadata, node.Labels), optionalPolicyTags...)
		}
	} else if spec.EnableIstioTags {
		tags = append(buildServiceTags(node.Metadata, node.Labels), optionalPolicyTags...)
	}

	if len(providerTags) == 0 {
		tags = append(tags, buildCustomTagsFromProxyConfig(proxyCfg.GetTracing().GetCustomTags())...)
	} else {
		tags = append(tags, buildCustomTagsFromProvider(providerTags)...)
	}

	// looping over customTags, a map, results in the returned value
	// being non-deterministic when multiple tags were defined; sort by the tag name
	// to rectify this
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Tag < tags[j].Tag
	})

	hcmTracing.CustomTags = tags
}

func buildCustomTagsFromProvider(providerTags map[string]*telemetrypb.Tracing_CustomTag) []*tracing.CustomTag {
	var tags []*tracing.CustomTag
	for tagName, tagInfo := range providerTags {
		if tagInfo == nil {
			log.Warnf("while building custom tags from provider, encountered nil custom tag: %s, skipping", tagName)
			continue
		}
		switch tag := tagInfo.Type.(type) {
		case *telemetrypb.Tracing_CustomTag_Environment:
			env := &tracing.CustomTag{
				Tag: tagName,
				Type: &tracing.CustomTag_Environment_{
					Environment: &tracing.CustomTag_Environment{
						Name:         tag.Environment.Name,
						DefaultValue: tag.Environment.DefaultValue,
					},
				},
			}
			tags = append(tags, env)
		case *telemetrypb.Tracing_CustomTag_Header:
			header := &tracing.CustomTag{
				Tag: tagName,
				Type: &tracing.CustomTag_RequestHeader{
					RequestHeader: &tracing.CustomTag_Header{
						Name:         tag.Header.Name,
						DefaultValue: tag.Header.DefaultValue,
					},
				},
			}
			tags = append(tags, header)
		case *telemetrypb.Tracing_CustomTag_Literal:
			env := &tracing.CustomTag{
				Tag: tagName,
				Type: &tracing.CustomTag_Literal_{
					Literal: &tracing.CustomTag_Literal{
						Value: tag.Literal.Value,
					},
				},
			}
			tags = append(tags, env)
		}
	}
	return tags
}

func buildCustomTagsFromProxyConfig(customTags map[string]*meshconfig.Tracing_CustomTag) []*tracing.CustomTag {
	var tags []*tracing.CustomTag

	for tagName, tagInfo := range customTags {
		if tagInfo == nil {
			log.Warnf("while building custom tags from proxyConfig, encountered nil custom tag: %s, skipping", tagName)
			continue
		}
		switch tag := tagInfo.Type.(type) {
		case *meshconfig.Tracing_CustomTag_Environment:
			env := &tracing.CustomTag{
				Tag: tagName,
				Type: &tracing.CustomTag_Environment_{
					Environment: &tracing.CustomTag_Environment{
						Name:         tag.Environment.Name,
						DefaultValue: tag.Environment.DefaultValue,
					},
				},
			}
			tags = append(tags, env)
		case *meshconfig.Tracing_CustomTag_Header:
			header := &tracing.CustomTag{
				Tag: tagName,
				Type: &tracing.CustomTag_RequestHeader{
					RequestHeader: &tracing.CustomTag_Header{
						Name:         tag.Header.Name,
						DefaultValue: tag.Header.DefaultValue,
					},
				},
			}
			tags = append(tags, header)
		case *meshconfig.Tracing_CustomTag_Literal:
			env := &tracing.CustomTag{
				Tag: tagName,
				Type: &tracing.CustomTag_Literal_{
					Literal: &tracing.CustomTag_Literal{
						Value: tag.Literal.Value,
					},
				},
			}
			tags = append(tags, env)
		}
	}
	return tags
}

func buildHTTPHeaders(headers []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader) []*core.HeaderValueOption {
	target := make([]*core.HeaderValueOption, 0, len(headers))
	for _, h := range headers {
		hvo := &core.HeaderValueOption{
			AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
			Header: &core.HeaderValue{
				Key:   h.GetName(),
				Value: getHeaderValue(h),
			},
		}
		target = append(target, hvo)
	}
	return target
}

func buildInitialMetadata(metadata []*meshconfig.MeshConfig_ExtensionProvider_HttpHeader) []*core.HeaderValue {
	if metadata == nil {
		return nil
	}
	target := make([]*core.HeaderValue, 0, len(metadata))
	for _, h := range metadata {
		hv := &core.HeaderValue{
			Key:   h.GetName(),
			Value: getHeaderValue(h),
		}
		target = append(target, hv)
	}
	return target
}

func getHeaderValue(header *meshconfig.MeshConfig_ExtensionProvider_HttpHeader) string {
	switch hv := header.HeaderValue.(type) {
	case *meshconfig.MeshConfig_ExtensionProvider_HttpHeader_Value:
		return hv.Value
	case *meshconfig.MeshConfig_ExtensionProvider_HttpHeader_EnvName:
		return env.Register[string](hv.EnvName, "", "").Get()
	}
	return ""
}
