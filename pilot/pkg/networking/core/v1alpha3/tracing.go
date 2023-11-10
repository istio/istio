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
	"fmt"
	"net/url"
	"sort"
	"strconv"

	opb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tracingcfg "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
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
	"istio.io/istio/pilot/pkg/xds/requestidextension"
	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/wellknown"
)

const (
	envoyDatadog       = "envoy.tracers.datadog"
	envoyOpenCensus    = "envoy.tracers.opencensus"
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
) (bool, *requestidextension.UUIDRequestIDExtensionContext) {
	tracing := push.Telemetry.Tracing(proxy)
	return configureTracingFromTelemetry(tracing, push, proxy, httpConnMgr, class)
}

func configureTracingFromTelemetry(
	tracing *model.TracingConfig,
	push *model.PushContext,
	proxy *model.Proxy,
	h *hcm.HttpConnectionManager,
	class networking.ListenerClass,
) (bool, *requestidextension.UUIDRequestIDExtensionContext) {
	proxyCfg := proxy.Metadata.ProxyConfigOrDefault(push.Mesh.DefaultConfig)
	// If there is no telemetry config defined, fallback to legacy mesh config.
	if tracing == nil {
		meshCfg := push.Mesh
		if !meshCfg.EnableTracing {
			log.Debug("No valid tracing configuration found")
			return false, nil
		}
		// use the prior configuration bits of sampling and custom tags
		h.Tracing = &hcm.HttpConnectionManager_Tracing{}
		configureSampling(h.Tracing, proxyConfigSamplingValue(proxyCfg))
		configureCustomTags(h.Tracing, map[string]*telemetrypb.Tracing_CustomTag{}, proxyCfg, proxy)
		if proxyCfg.GetTracing().GetMaxPathTagLength() != 0 {
			h.Tracing.MaxPathTagLength = wrapperspb.UInt32(proxyCfg.GetTracing().MaxPathTagLength)
		}
		return false, nil
	}
	spec := tracing.ServerSpec
	if class == networking.ListenerClassSidecarOutbound || class == networking.ListenerClassGateway {
		spec = tracing.ClientSpec
	}

	if spec.Disabled {
		return false, nil
	}

	var startChildSpan bool
	if spec.Provider != nil {
		tcfg, child, err := configureFromProviderConfig(push, proxy, spec.Provider)
		if err != nil {
			log.Warnf("Not able to configure requested tracing provider %q: %v", spec.Provider.Name, err)
			return false, nil
		}
		h.Tracing = tcfg
		startChildSpan = child
	} else {
		// TODO: should this `return nil, nil` instead ?
		log.Warnf("Not able to configure tracing provider. Provider lookup failed.")
		h.Tracing = &hcm.HttpConnectionManager_Tracing{}
		// TODO: transition to configuring providers from proxy config here?
		// something like: configureFromProxyConfig(tracingCfg, opts.proxy.Metadata.ProxyConfig.Tracing)
	}

	var sampling float64
	if spec.RandomSamplingPercentage != nil {
		sampling = *spec.RandomSamplingPercentage
	} else {
		// gracefully fallback to MeshConfig configuration. It will act as an implicit
		// parent configuration during transition period.
		sampling = proxyConfigSamplingValue(proxyCfg)
	}

	configureSampling(h.Tracing, sampling)
	configureCustomTags(h.Tracing, spec.CustomTags, proxyCfg, proxy)

	// if there is configured max tag length somewhere, fallback to it.
	if h.GetTracing().GetMaxPathTagLength() == nil && proxyCfg.GetTracing().GetMaxPathTagLength() != 0 {
		h.Tracing.MaxPathTagLength = wrapperspb.UInt32(proxyCfg.GetTracing().MaxPathTagLength)
	}

	reqIDExtension := &requestidextension.UUIDRequestIDExtensionContext{}
	reqIDExtension.UseRequestIDForTraceSampling = spec.UseRequestIDForTraceSampling
	return startChildSpan, reqIDExtension
}

// configureFromProviderConfigHandled contains the number of providers we handle below.
// This is to ensure this stays in sync as new handlers are added
// STOP. DO NOT UPDATE THIS WITHOUT UPDATING configureFromProviderConfig.
const configureFromProviderConfigHandled = 14

func configureFromProviderConfig(pushCtx *model.PushContext, proxy *model.Proxy,
	providerCfg *meshconfig.MeshConfig_ExtensionProvider,
) (*hcm.HttpConnectionManager_Tracing, bool, error) {
	startChildSpan := false
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
			return zipkinConfig(hostname, cluster, !provider.Zipkin.GetEnable_64BitTraceId())
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
		//nolint: staticcheck  // Lightstep deprecated
		maxTagLength = provider.Lightstep.GetMaxTagLength()
		providerName = envoyOpenTelemetry
		//nolint: staticcheck  // Lightstep deprecated
		providerConfig = func() (*anypb.Any, error) {
			hostname, clusterName, err := clusterLookupFn(pushCtx, provider.Lightstep.GetService(), int(provider.Lightstep.GetPort()))
			if err != nil {
				model.IncLookupClusterFailures("lightstep")
				return nil, fmt.Errorf("could not find cluster for tracing provider %q: %v", provider, err)
			}
			return otelLightStepConfig(clusterName, hostname, provider.Lightstep.GetAccessToken())
		}
	case *meshconfig.MeshConfig_ExtensionProvider_Opencensus:
		maxTagLength = provider.Opencensus.GetMaxTagLength()
		providerName = envoyOpenCensus
		providerConfig = func() (*anypb.Any, error) {
			return opencensusConfig(provider.Opencensus)
		}
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
	case *meshconfig.MeshConfig_ExtensionProvider_Stackdriver:
		maxTagLength = provider.Stackdriver.GetMaxTagLength()
		providerName = envoyOpenCensus
		providerConfig = func() (*anypb.Any, error) {
			return stackdriverConfig(proxy.Metadata, provider.Stackdriver)
		}
	case *meshconfig.MeshConfig_ExtensionProvider_Opentelemetry:
		maxTagLength = provider.Opentelemetry.GetMaxTagLength()
		providerName = envoyOpenTelemetry
		providerConfig = func() (*anypb.Any, error) {
			hostname, clusterName, err := clusterLookupFn(pushCtx, provider.Opentelemetry.GetService(), int(provider.Opentelemetry.GetPort()))
			if err != nil {
				model.IncLookupClusterFailures("opentelemetry")
				return nil, fmt.Errorf("could not find cluster for tracing provider %q: %v", provider, err)
			}
			return otelConfig(serviceCluster, hostname, clusterName, provider.Opentelemetry)
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
	tracing, err := buildHCMTracing(providerName, maxTagLength, providerConfig)
	return tracing, startChildSpan, err
}

func zipkinConfig(hostname, cluster string, enable128BitTraceID bool) (*anypb.Any, error) {
	zc := &tracingcfg.ZipkinConfig{
		CollectorCluster:         cluster,
		CollectorEndpoint:        "/api/v2/spans",                   // envoy deprecated v1 support
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

func otelConfig(serviceName, hostname, cluster string, otelProvider *meshconfig.MeshConfig_ExtensionProvider_OpenTelemetryTracingProvider) (*anypb.Any, error) {
	var oc *tracingcfg.OpenTelemetryConfig

	if otelProvider.GetHttp() == nil {
		oc = &tracingcfg.OpenTelemetryConfig{
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: cluster,
						Authority:   hostname,
					},
				},
			},
		}
	} else {
		httpService := otelProvider.GetHttp()
		te, err := url.JoinPath(hostname, httpService.GetPath())
		if err != nil {
			return nil, fmt.Errorf("could not parse otlp/http traces endpoint: %v", err)
		}
		oc = &tracingcfg.OpenTelemetryConfig{
			HttpService: &core.HttpService{
				HttpUri: &core.HttpUri{
					Uri: te,
					HttpUpstreamType: &core.HttpUri_Cluster{
						Cluster: cluster,
					},
					Timeout: httpService.GetTimeout(),
				},
			},
		}

		for _, h := range httpService.GetHeaders() {
			hvo := &core.HeaderValueOption{
				AppendAction: core.HeaderValueOption_OVERWRITE_IF_EXISTS_OR_ADD,
				Header: &core.HeaderValue{
					Key:   h.GetName(),
					Value: h.GetValue(),
				},
			}
			oc.GetHttpService().RequestHeadersToAdd = append(oc.GetHttpService().GetRequestHeadersToAdd(), hvo)
		}
	}

	oc.ServiceName = serviceName
	return anypb.New(oc)
}

func opencensusConfig(opencensusProvider *meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider) (*anypb.Any, error) {
	oc := &tracingcfg.OpenCensusConfig{
		OcagentAddress:         fmt.Sprintf("%s:%d", opencensusProvider.GetService(), opencensusProvider.GetPort()),
		OcagentExporterEnabled: true,
		// this is incredibly dangerous for proxy stability, as switching provider config for OC providers
		// is not allowed during the lifetime of a proxy.
		IncomingTraceContext: convert(opencensusProvider.GetContext()),
		OutgoingTraceContext: convert(opencensusProvider.GetContext()),
	}

	return protoconv.MessageToAnyWithError(oc)
}

func stackdriverConfig(proxyMetaData *model.NodeMetadata, sdProvider *meshconfig.MeshConfig_ExtensionProvider_StackdriverProvider) (*anypb.Any, error) {
	proj, ok := proxyMetaData.PlatformMetadata[platform.GCPProject]
	if !ok {
		proj, ok = proxyMetaData.PlatformMetadata[platform.GCPProjectNumber]
	}
	if !ok {
		return nil, fmt.Errorf("could not configure Stackdriver tracer - unknown project id")
	}

	sd := &tracingcfg.OpenCensusConfig{
		StackdriverExporterEnabled: true,
		StackdriverProjectId:       proj,
		IncomingTraceContext:       allContexts,
		OutgoingTraceContext:       allContexts,
		// supporting dynamic control is considered harmful, as OC can only be configured once per lifetime
		StdoutExporterEnabled: false,
		TraceConfig: &opb.TraceConfig{
			MaxNumberOfAnnotations:   200,
			MaxNumberOfAttributes:    200,
			MaxNumberOfMessageEvents: 200,
		},
	}

	if proxyMetaData.StsPort != "" {
		stsPort, err := strconv.Atoi(proxyMetaData.StsPort)
		if err != nil || stsPort < 1 {
			return nil, fmt.Errorf("could not configure Stackdriver tracer - bad sts port: %v", err)
		}
		tokenPath := constants.TrustworthyJWTPath
		// nolint: staticcheck
		sd.StackdriverGrpcService = &core.GrpcService{
			InitialMetadata: []*core.HeaderValue{
				{
					Key:   "x-goog-user-project",
					Value: proj,
				},
			},
			TargetSpecifier: &core.GrpcService_GoogleGrpc_{
				GoogleGrpc: &core.GrpcService_GoogleGrpc{
					TargetUri:  "cloudtrace.googleapis.com",
					StatPrefix: "oc_stackdriver_tracer",
					ChannelCredentials: &core.GrpcService_GoogleGrpc_ChannelCredentials{
						CredentialSpecifier: &core.GrpcService_GoogleGrpc_ChannelCredentials_SslCredentials{
							SslCredentials: &core.GrpcService_GoogleGrpc_SslCredentials{},
						},
					},
					CallCredentials: []*core.GrpcService_GoogleGrpc_CallCredentials{
						{
							CredentialSpecifier: &core.GrpcService_GoogleGrpc_CallCredentials_StsService_{
								StsService: &core.GrpcService_GoogleGrpc_CallCredentials_StsService{
									TokenExchangeServiceUri: fmt.Sprintf("http://localhost:%d/token", stsPort),
									SubjectTokenPath:        tokenPath,
									SubjectTokenType:        "urn:ietf:params:oauth:token-type:jwt",
									Scope:                   "https://www.googleapis.com/auth/cloud-platform",
								},
							},
						},
					},
				},
			},
		}
	}

	// supporting dynamic control is considered harmful, as OC can only be configured once per lifetime
	// so, we should not allow dynamic control based on provider configuration of the following params:
	// - max number of annotations
	// - max number of attributes
	// - max number of message events
	// The following code block allows control for a single configuration once during the lifecycle of a
	// mesh.
	// nolint: staticcheck
	if sdProvider.GetMaxNumberOfAnnotations() != nil {
		sd.TraceConfig.MaxNumberOfAnnotations = sdProvider.GetMaxNumberOfAnnotations().GetValue()
	}
	// nolint: staticcheck
	if sdProvider.GetMaxNumberOfAttributes() != nil {
		sd.TraceConfig.MaxNumberOfAttributes = sdProvider.GetMaxNumberOfAttributes().GetValue()
	}
	// nolint: staticcheck
	if sdProvider.GetMaxNumberOfMessageEvents() != nil {
		sd.TraceConfig.MaxNumberOfMessageEvents = sdProvider.GetMaxNumberOfMessageEvents().GetValue()
	}
	return protoconv.MessageToAnyWithError(sd)
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

func otelLightStepConfig(clusterName, hostname, accessToken string) (*anypb.Any, error) {
	dc := &tracingcfg.OpenTelemetryConfig{
		GrpcService: &core.GrpcService{
			TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
				EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
					ClusterName: clusterName,
					Authority:   hostname,
				},
			},
			InitialMetadata: []*core.HeaderValue{
				{
					Key:   "lightstep-access-token",
					Value: accessToken,
				},
			},
		},
	}
	return anypb.New(dc)
}

func buildHCMTracing(provider string, maxTagLen uint32, anyFn typedConfigGenFn) (*hcm.HttpConnectionManager_Tracing, error) {
	config := &hcm.HttpConnectionManager_Tracing{}
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

var allContexts = []tracingcfg.OpenCensusConfig_TraceContext{
	tracingcfg.OpenCensusConfig_B3,
	tracingcfg.OpenCensusConfig_CLOUD_TRACE_CONTEXT,
	tracingcfg.OpenCensusConfig_GRPC_TRACE_BIN,
	tracingcfg.OpenCensusConfig_TRACE_CONTEXT,
}

func convert(ctxs []meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider_TraceContext) []tracingcfg.OpenCensusConfig_TraceContext {
	if len(ctxs) == 0 {
		return allContexts
	}
	converted := make([]tracingcfg.OpenCensusConfig_TraceContext, 0, len(ctxs))
	for _, c := range ctxs {
		switch c {
		case meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider_B3:
			converted = append(converted, tracingcfg.OpenCensusConfig_B3)
		case meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider_CLOUD_TRACE_CONTEXT:
			converted = append(converted, tracingcfg.OpenCensusConfig_CLOUD_TRACE_CONTEXT)
		case meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider_GRPC_BIN:
			converted = append(converted, tracingcfg.OpenCensusConfig_GRPC_TRACE_BIN)
		case meshconfig.MeshConfig_ExtensionProvider_OpenCensusAgentTracingProvider_W3C_TRACE_CONTEXT:
			converted = append(converted, tracingcfg.OpenCensusConfig_TRACE_CONTEXT)
		}
	}
	return converted
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
	sampling := features.TraceSampling

	if config.Tracing != nil && config.Tracing.Sampling != 0.0 {
		sampling = config.Tracing.Sampling

		if sampling > 100.0 {
			sampling = 1.0
		}
	}
	return sampling
}

func configureCustomTags(hcmTracing *hcm.HttpConnectionManager_Tracing,
	providerTags map[string]*telemetrypb.Tracing_CustomTag, proxyCfg *meshconfig.ProxyConfig, node *model.Proxy,
) {
	tags := append(buildServiceTags(node.Metadata, node.Labels), optionalPolicyTags...)

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
