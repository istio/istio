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
	"sort"
	"strconv"

	opb "github.com/census-instrumentation/opencensus-proto/gen-go/trace/v1"
	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tracingcfg "github.com/envoyproxy/go-control-plane/envoy/config/trace/v3"
	hpb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	envoy_type_metadata_v3 "github.com/envoyproxy/go-control-plane/envoy/type/metadata/v3"
	tracing "github.com/envoyproxy/go-control-plane/envoy/type/tracing/v3"
	xdstype "github.com/envoyproxy/go-control-plane/envoy/type/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	telemetrypb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/extensionproviders"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	authz_model "istio.io/istio/pilot/pkg/security/authz/model"
	xdsfilters "istio.io/istio/pilot/pkg/xds/filters"
	"istio.io/istio/pilot/pkg/xds/requestidextension"
	"istio.io/istio/pkg/bootstrap/platform"
	"istio.io/istio/pkg/config/constants"
	"istio.io/pkg/log"
)

// this is used for testing. it should not be changed in regular code.
var clusterLookupFn = extensionproviders.LookupCluster

func configureTracing(
	push *model.PushContext,
	proxy *model.Proxy,
	hcm *hpb.HttpConnectionManager,
	class networking.ListenerClass,
) (*xdsfilters.RouterFilterContext, *requestidextension.UUIDRequestIDExtensionContext) {
	tracing := push.Telemetry.Tracing(proxy)
	return configureTracingFromSpec(tracing, push, proxy, hcm, class)
}

func configureTracingFromSpec(
	tracing *model.TracingConfig,
	push *model.PushContext,
	proxy *model.Proxy,
	hcm *hpb.HttpConnectionManager,
	class networking.ListenerClass,
) (*xdsfilters.RouterFilterContext, *requestidextension.UUIDRequestIDExtensionContext) {
	meshCfg := push.Mesh
	proxyCfg := proxy.Metadata.ProxyConfigOrDefault(push.Mesh.DefaultConfig)

	if tracing == nil {
		// No Telemetry config for tracing, fallback to legacy mesh config
		if !meshCfg.EnableTracing {
			log.Debug("No valid tracing configuration found")
			return nil, nil
		}
		// use the prior configuration bits of sampling and custom tags
		hcm.Tracing = &hpb.HttpConnectionManager_Tracing{}
		configureSampling(hcm.Tracing, proxyConfigSamplingValue(proxyCfg))
		configureCustomTags(hcm.Tracing, map[string]*telemetrypb.Tracing_CustomTag{}, proxyCfg, proxy)
		if proxyCfg.GetTracing().GetMaxPathTagLength() != 0 {
			hcm.Tracing.MaxPathTagLength = wrapperspb.UInt32(proxyCfg.GetTracing().MaxPathTagLength)
		}
		return nil, nil
	}

	spec := tracing.ServerSpec
	if class == networking.ListenerClassSidecarOutbound || class == networking.ListenerClassGateway {
		spec = tracing.ClientSpec
	}

	if spec.Disabled {
		return nil, nil
	}

	var routerFilterCtx *xdsfilters.RouterFilterContext
	if spec.Provider != nil {
		tcfg, rfCtx, err := configureFromProviderConfig(push, proxy.Metadata, spec.Provider)
		if err != nil {
			log.Warnf("Not able to configure requested tracing provider %q: %v", spec.Provider.Name, err)
			return nil, nil
		}
		hcm.Tracing = tcfg
		routerFilterCtx = rfCtx
	} else {
		// TODO: should this `return nil, nil` instead ?
		log.Warnf("Not able to configure tracing provider. Provider lookup failed.")
		hcm.Tracing = &hpb.HttpConnectionManager_Tracing{}
		// TODO: transition to configuring providers from proxy config here?
		// something like: configureFromProxyConfig(tracingCfg, opts.proxy.Metadata.ProxyConfig.Tracing)
	}

	// gracefully fallback to MeshConfig configuration. It will act as an implicit
	// parent configuration during transition period.
	configureSampling(hcm.Tracing, spec.RandomSamplingPercentage)
	configureCustomTags(hcm.Tracing, spec.CustomTags, proxyCfg, proxy)

	// if there is configured max tag length somewhere, fallback to it.
	if hcm.GetTracing().GetMaxPathTagLength() == nil && proxyCfg.GetTracing().GetMaxPathTagLength() != 0 {
		hcm.Tracing.MaxPathTagLength = wrapperspb.UInt32(proxyCfg.GetTracing().MaxPathTagLength)
	}

	reqIDExtension := &requestidextension.UUIDRequestIDExtensionContext{}
	reqIDExtension.UseRequestIDForTraceSampling = spec.UseRequestIDForTraceSampling
	return routerFilterCtx, reqIDExtension
}

// TODO: follow-on work to enable bootstrapping of clusters for $(HOST_IP):PORT addresses.

func configureFromProviderConfig(pushCtx *model.PushContext, meta *model.NodeMetadata,
	providerCfg *meshconfig.MeshConfig_ExtensionProvider,
) (*hpb.HttpConnectionManager_Tracing, *xdsfilters.RouterFilterContext, error) {
	tracing := &hpb.HttpConnectionManager_Tracing{}
	var rfCtx *xdsfilters.RouterFilterContext
	var err error

	switch provider := providerCfg.Provider.(type) {
	case *meshconfig.MeshConfig_ExtensionProvider_Zipkin:
		tracing, err = buildHCMTracing(pushCtx, providerCfg.Name, provider.Zipkin.Service, provider.Zipkin.Port, provider.Zipkin.MaxTagLength, zipkinConfigGen)
	case *meshconfig.MeshConfig_ExtensionProvider_Datadog:
		tracing, err = buildHCMTracing(pushCtx, providerCfg.Name, provider.Datadog.Service, provider.Datadog.Port, provider.Datadog.MaxTagLength, datadogConfigGen)
	case *meshconfig.MeshConfig_ExtensionProvider_Lightstep:
		tracing, err = buildHCMTracing(pushCtx, providerCfg.Name, provider.Lightstep.Service, provider.Lightstep.Port, provider.Lightstep.MaxTagLength,
			func(hostname, clusterName string) (*anypb.Any, error) {
				lc := &tracingcfg.LightstepConfig{
					CollectorCluster: clusterName,
					AccessTokenFile:  provider.Lightstep.AccessToken,
				}
				return anypb.New(lc)
			})

	case *meshconfig.MeshConfig_ExtensionProvider_Opencensus:
		tracing, err = buildHCMTracingOpenCensus(providerCfg.Name, provider.Opencensus.MaxTagLength, func() (*anypb.Any, error) {
			oc := &tracingcfg.OpenCensusConfig{
				OcagentAddress:         fmt.Sprintf("%s:%d", provider.Opencensus.Service, provider.Opencensus.Port),
				OcagentExporterEnabled: true,
				IncomingTraceContext:   convert(provider.Opencensus.Context),
				OutgoingTraceContext:   convert(provider.Opencensus.Context),
			}

			return anypb.New(oc)
		})

	case *meshconfig.MeshConfig_ExtensionProvider_Skywalking:
		tracing, err = buildHCMTracing(pushCtx, providerCfg.Name, provider.Skywalking.Service,
			provider.Skywalking.Port, 0, func(hostname, clusterName string) (*anypb.Any, error) {
				s := &tracingcfg.SkyWalkingConfig{
					GrpcService: &envoy_config_core_v3.GrpcService{
						TargetSpecifier: &envoy_config_core_v3.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &envoy_config_core_v3.GrpcService_EnvoyGrpc{
								ClusterName: clusterName,
								Authority:   hostname,
							},
						},
					},
				}

				return anypb.New(s)
			})

		rfCtx = &xdsfilters.RouterFilterContext{
			StartChildSpan: true,
		}

	case *meshconfig.MeshConfig_ExtensionProvider_Stackdriver:
		tracing, err = buildHCMTracingOpenCensus(providerCfg.Name, provider.Stackdriver.MaxTagLength, func() (*anypb.Any, error) {
			proj, ok := meta.PlatformMetadata[platform.GCPProject]
			if !ok {
				proj, ok = meta.PlatformMetadata[platform.GCPProjectNumber]
			}
			if !ok {
				return nil, fmt.Errorf("could not configure Stackdriver tracer - unknown project id")
			}

			sd := &tracingcfg.OpenCensusConfig{
				StackdriverExporterEnabled: true,
				StackdriverProjectId:       proj,
				IncomingTraceContext:       allContexts,
				OutgoingTraceContext:       allContexts,
				StdoutExporterEnabled:      provider.Stackdriver.Debug,
				TraceConfig: &opb.TraceConfig{
					MaxNumberOfAnnotations:   200,
					MaxNumberOfAttributes:    200,
					MaxNumberOfMessageEvents: 200,
				},
			}

			if meta.StsPort != "" {
				stsPort, err := strconv.Atoi(meta.StsPort)
				if err != nil || stsPort < 1 {
					return nil, fmt.Errorf("could not configure Stackdriver tracer - bad sts port: %v", err)
				}
				sd.StackdriverGrpcService = &envoy_config_core_v3.GrpcService{
					InitialMetadata: []*envoy_config_core_v3.HeaderValue{
						{
							Key:   "x-goog-user-project",
							Value: proj,
						},
					},
					TargetSpecifier: &envoy_config_core_v3.GrpcService_GoogleGrpc_{
						GoogleGrpc: &envoy_config_core_v3.GrpcService_GoogleGrpc{
							TargetUri:  "cloudtrace.googleapis.com",
							StatPrefix: "oc_stackdriver_tracer",
							ChannelCredentials: &envoy_config_core_v3.GrpcService_GoogleGrpc_ChannelCredentials{
								CredentialSpecifier: &envoy_config_core_v3.GrpcService_GoogleGrpc_ChannelCredentials_SslCredentials{
									SslCredentials: &envoy_config_core_v3.GrpcService_GoogleGrpc_SslCredentials{},
								},
							},
							CallCredentials: []*envoy_config_core_v3.GrpcService_GoogleGrpc_CallCredentials{
								{
									CredentialSpecifier: &envoy_config_core_v3.GrpcService_GoogleGrpc_CallCredentials_StsService_{
										StsService: &envoy_config_core_v3.GrpcService_GoogleGrpc_CallCredentials_StsService{
											TokenExchangeServiceUri: fmt.Sprintf("http://localhost:%d/token", stsPort),
											SubjectTokenPath:        constants.TrustworthyJWTPath,
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

			if provider.Stackdriver.MaxNumberOfAnnotations != nil {
				sd.TraceConfig.MaxNumberOfAnnotations = provider.Stackdriver.MaxNumberOfAnnotations.Value
			}
			if provider.Stackdriver.MaxNumberOfAttributes != nil {
				sd.TraceConfig.MaxNumberOfAttributes = provider.Stackdriver.MaxNumberOfAttributes.Value
			}
			if provider.Stackdriver.MaxNumberOfMessageEvents != nil {
				sd.TraceConfig.MaxNumberOfMessageEvents = provider.Stackdriver.MaxNumberOfMessageEvents.Value
			}
			return anypb.New(sd)
		})
	}

	return tracing, rfCtx, err
}

type typedConfigGenFromClusterFn func(hostname, clusterName string) (*anypb.Any, error)

func zipkinConfigGen(hostname, cluster string) (*anypb.Any, error) {
	zc := &tracingcfg.ZipkinConfig{
		CollectorCluster:         cluster,
		CollectorEndpoint:        "/api/v2/spans",                   // envoy deprecated v1 support
		CollectorEndpointVersion: tracingcfg.ZipkinConfig_HTTP_JSON, // use v2 JSON for now
		CollectorHostname:        hostname,                          // http host header
		TraceId_128Bit:           true,
		SharedSpanContext:        wrapperspb.Bool(false),
	}
	return anypb.New(zc)
}

func datadogConfigGen(hostname, cluster string) (*anypb.Any, error) {
	dc := &tracingcfg.DatadogConfig{
		CollectorCluster: cluster,
	}
	return anypb.New(dc)
}

type typedConfigGenFn func() (*anypb.Any, error)

func buildHCMTracing(pushCtx *model.PushContext, provider, svc string, port, maxTagLen uint32,
	anyFn typedConfigGenFromClusterFn,
) (*hpb.HttpConnectionManager_Tracing, error) {
	config := &hpb.HttpConnectionManager_Tracing{}

	hostname, cluster, err := clusterLookupFn(pushCtx, svc, int(port))
	if err != nil {
		return config, fmt.Errorf("could not find cluster for tracing provider %q: %v", provider, err)
	}

	cfg, err := anyFn(hostname, cluster)
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

func buildHCMTracingOpenCensus(provider string, maxTagLen uint32, anyFn typedConfigGenFn) (*hpb.HttpConnectionManager_Tracing, error) {
	config := &hpb.HttpConnectionManager_Tracing{}
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

func buildOptionalPolicyTags() []*tracing.CustomTag {
	return []*tracing.CustomTag{
		dryRunPolicyTraceTag("istio.authorization.dry_run.allow_policy.name", authz_model.RBACShadowRulesAllowStatPrefix+authz_model.RBACShadowEffectivePolicyID),
		dryRunPolicyTraceTag("istio.authorization.dry_run.allow_policy.result", authz_model.RBACShadowRulesAllowStatPrefix+authz_model.RBACShadowEngineResult),
		dryRunPolicyTraceTag("istio.authorization.dry_run.deny_policy.name", authz_model.RBACShadowRulesDenyStatPrefix+authz_model.RBACShadowEffectivePolicyID),
		dryRunPolicyTraceTag("istio.authorization.dry_run.deny_policy.result", authz_model.RBACShadowRulesDenyStatPrefix+authz_model.RBACShadowEngineResult),
	}
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
	}
}

func configureSampling(hcmTracing *hpb.HttpConnectionManager_Tracing, providerPercentage float64) {
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

func configureCustomTags(hcmTracing *hpb.HttpConnectionManager_Tracing,
	providerTags map[string]*telemetrypb.Tracing_CustomTag, proxyCfg *meshconfig.ProxyConfig, node *model.Proxy,
) {
	var tags []*tracing.CustomTag

	// TODO(dougreid): remove support for this feature. We don't want this to be
	// optional moving forward. And we can add it back in via the Telemetry API
	// later, if needed.
	// THESE TAGS SHOULD BE ALWAYS ON.
	if features.EnableIstioTags {
		tags = append(tags, buildOptionalPolicyTags()...)
		tags = append(tags, buildServiceTags(node.Metadata, node.Labels)...)
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
