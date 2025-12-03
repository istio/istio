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
	"strings"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	grpcaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/grpc/v3"
	otelaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/open_telemetry/v3"
	celformatter "github.com/envoyproxy/go-control-plane/envoy/extensions/formatter/cel/v3"
	metadataformatter "github.com/envoyproxy/go-control-plane/envoy/extensions/formatter/metadata/v3"
	reqwithoutquery "github.com/envoyproxy/go-control-plane/envoy/extensions/formatter/req_without_query/v3"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/types/known/structpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/wellknown"
)

const (
	// EnvoyTextLogFormat format for envoy text based access logs for Istio 1.9 onwards.
	// This includes the additional new operator RESPONSE_CODE_DETAILS and CONNECTION_TERMINATION_DETAILS that tells
	// the reason why Envoy rejects a request.
	EnvoyTextLogFormat = "[%START_TIME%] \"%REQ(:METHOD)% %REQ(X-ENVOY-ORIGINAL-PATH?:PATH)% " +
		"%PROTOCOL%\" %RESPONSE_CODE% %RESPONSE_FLAGS% " +
		"%RESPONSE_CODE_DETAILS% %CONNECTION_TERMINATION_DETAILS% " +
		"\"%UPSTREAM_TRANSPORT_FAILURE_REASON%\" %BYTES_RECEIVED% %BYTES_SENT% " +
		"%DURATION% %RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)% \"%REQ(X-FORWARDED-FOR)%\" " +
		"\"%REQ(USER-AGENT)%\" \"%REQ(X-REQUEST-ID)%\" \"%REQ(:AUTHORITY)%\" \"%UPSTREAM_HOST%\" " +
		"%UPSTREAM_CLUSTER_RAW% %UPSTREAM_LOCAL_ADDRESS% %DOWNSTREAM_LOCAL_ADDRESS% " +
		"%DOWNSTREAM_REMOTE_ADDRESS% %REQUESTED_SERVER_NAME% %ROUTE_NAME%\n"

	HTTPEnvoyAccessLogFriendlyName = "http_envoy_accesslog"
	TCPEnvoyAccessLogFriendlyName  = "tcp_envoy_accesslog"
	OtelEnvoyAccessLogFriendlyName = "otel_envoy_accesslog"

	TCPEnvoyALSName  = "envoy.tcp_grpc_access_log"
	OtelEnvoyALSName = "envoy.access_loggers.open_telemetry"

	reqWithoutQueryCommandOperator = "%REQ_WITHOUT_QUERY"
	metadataCommandOperator        = "%METADATA"
	celCommandOperator             = "%CEL"
	// count of all supported formatter, right now is 3(CEL, METADATA and REQ_WITHOUT_QUERY).
	maxFormattersLength = 3

	DevStdout = "/dev/stdout"

	builtinEnvoyAccessLogProvider = "envoy"
)

var (
	// this is used for testing. it should not be changed in regular code.
	clusterLookupFn = LookupCluster

	// EnvoyJSONLogFormatIstio map of values for envoy json based access logs for Istio 1.9 onwards.
	// This includes the additional log operator RESPONSE_CODE_DETAILS and CONNECTION_TERMINATION_DETAILS that tells
	// the reason why Envoy rejects a request.
	EnvoyJSONLogFormatIstio = &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"start_time":                        {Kind: &structpb.Value_StringValue{StringValue: "%START_TIME%"}},
			"route_name":                        {Kind: &structpb.Value_StringValue{StringValue: "%ROUTE_NAME%"}},
			"method":                            {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:METHOD)%"}},
			"path":                              {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"}},
			"protocol":                          {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
			"response_code":                     {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE%"}},
			"response_flags":                    {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_FLAGS%"}},
			"response_code_details":             {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE_DETAILS%"}},
			"connection_termination_details":    {Kind: &structpb.Value_StringValue{StringValue: "%CONNECTION_TERMINATION_DETAILS%"}},
			"bytes_received":                    {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_RECEIVED%"}},
			"bytes_sent":                        {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_SENT%"}},
			"duration":                          {Kind: &structpb.Value_StringValue{StringValue: "%DURATION%"}},
			"upstream_service_time":             {Kind: &structpb.Value_StringValue{StringValue: "%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%"}},
			"x_forwarded_for":                   {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-FORWARDED-FOR)%"}},
			"user_agent":                        {Kind: &structpb.Value_StringValue{StringValue: "%REQ(USER-AGENT)%"}},
			"request_id":                        {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-REQUEST-ID)%"}},
			"authority":                         {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:AUTHORITY)%"}},
			"upstream_host":                     {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_HOST%"}},
			"upstream_cluster":                  {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_CLUSTER_RAW%"}},
			"upstream_local_address":            {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_LOCAL_ADDRESS%"}},
			"downstream_local_address":          {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_LOCAL_ADDRESS%"}},
			"downstream_remote_address":         {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_REMOTE_ADDRESS%"}},
			"requested_server_name":             {Kind: &structpb.Value_StringValue{StringValue: "%REQUESTED_SERVER_NAME%"}},
			"upstream_transport_failure_reason": {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_TRANSPORT_FAILURE_REASON%"}},
		},
	}

	// State logged by the metadata exchange filter about the upstream and downstream service instances
	// We need to propagate these as part of access log service stream
	// Logging them by default on the console may be an issue as the base64 encoded string is bound to be a big one.
	// But end users can certainly configure it on their own via the meshConfig using the %FILTER_STATE% macro.
	// Related to https://github.com/istio/proxy/pull/5825, start from 1.24, Istio use new filter state key.
	envoyStateToLog     = []string{"upstream_peer", "downstream_peer"}
	envoyWasmStateToLog = []string{"wasm.upstream_peer", "wasm.upstream_peer_id", "wasm.downstream_peer", "wasm.downstream_peer_id"}

	// reqWithoutQueryFormatter configures additional formatters needed for some of the format strings like "REQ_WITHOUT_QUERY"
	reqWithoutQueryFormatter = &core.TypedExtensionConfig{
		Name:        "envoy.formatter.req_without_query",
		TypedConfig: protoconv.MessageToAny(&reqwithoutquery.ReqWithoutQuery{}),
	}

	// metadataFormatter configures additional formatters needed for some of the format strings like "METADATA"
	// for more information, see https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/formatter/metadata/v3/metadata.proto
	metadataFormatter = &core.TypedExtensionConfig{
		Name:        "envoy.formatter.metadata",
		TypedConfig: protoconv.MessageToAny(&metadataformatter.Metadata{}),
	}

	// celFormatter configures additional formatters needed for some of the format strings like "CEL"
	// for more information, see https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/formatter/cel/v3/cel.proto
	celFormatter = &core.TypedExtensionConfig{
		Name:        "envoy.formatter.cel",
		TypedConfig: protoconv.MessageToAny(&celformatter.Cel{}),
	}
)

// configureFromProviderConfigHandled contains the number of providers we handle below.
// This is to ensure this stays in sync as new handlers are added
// STOP. DO NOT UPDATE THIS WITHOUT UPDATING telemetryAccessLog.
const telemetryAccessLogHandled = 15

func telemetryAccessLog(push *PushContext, proxy *Proxy, fp *meshconfig.MeshConfig_ExtensionProvider) *accesslog.AccessLog {
	var al *accesslog.AccessLog
	switch prov := fp.Provider.(type) {
	case *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog:
		// For built-in provider, fallback to MeshConfig for formatting options when LogFormat unset.
		if fp.Name == builtinEnvoyAccessLogProvider &&
			prov.EnvoyFileAccessLog.LogFormat == nil && !prov.EnvoyFileAccessLog.OmitEmptyValues {
			al = FileAccessLogFromMeshConfig(prov.EnvoyFileAccessLog.Path, push.Mesh)
		} else {
			al = fileAccessLogFromTelemetry(prov.EnvoyFileAccessLog)
		}
	case *meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpAls:
		al = httpGrpcAccessLogFromTelemetry(push, proxy, prov.EnvoyHttpAls)
	case *meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpAls:
		al = tcpGrpcAccessLogFromTelemetry(push, proxy, prov.EnvoyTcpAls)
	case *meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls:
		al = openTelemetryLog(push, proxy, prov.EnvoyOtelAls)
	case *meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzHttp,
		*meshconfig.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc,
		*meshconfig.MeshConfig_ExtensionProvider_Zipkin,
		*meshconfig.MeshConfig_ExtensionProvider_Lightstep,
		*meshconfig.MeshConfig_ExtensionProvider_Datadog,
		*meshconfig.MeshConfig_ExtensionProvider_Skywalking,
		*meshconfig.MeshConfig_ExtensionProvider_Opencensus,
		*meshconfig.MeshConfig_ExtensionProvider_Opentelemetry,
		*meshconfig.MeshConfig_ExtensionProvider_Prometheus,
		*meshconfig.MeshConfig_ExtensionProvider_Sds,
		*meshconfig.MeshConfig_ExtensionProvider_Stackdriver:
		// No access logs supported for this provider
		// Stackdriver is a special case as its handled in the Metrics logic, as it uses a shared filter
		return nil
	}

	return al
}

func filterStateObjectsToLog(proxy *Proxy) []string {
	if proxy.VersionGreaterOrEqual(&IstioVersion{Major: 1, Minor: 24, Patch: 0}) {
		return envoyStateToLog
	}

	return envoyWasmStateToLog
}

func tcpGrpcAccessLogFromTelemetry(push *PushContext, proxy *Proxy,
	prov *meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider,
) *accesslog.AccessLog {
	logName := TCPEnvoyAccessLogFriendlyName
	if prov != nil && prov.LogName != "" {
		logName = prov.LogName
	}

	filterObjects := filterStateObjectsToLog(proxy)
	if len(prov.FilterStateObjectsToLog) != 0 {
		filterObjects = prov.FilterStateObjectsToLog
	}

	hostname, cluster, err := clusterLookupFn(push, prov.Service, int(prov.Port))
	if err != nil {
		IncLookupClusterFailures("envoyTCPAls")
		log.Errorf("could not find cluster for tcp grpc provider %q: %v", prov, err)
		return nil
	}

	fl := &grpcaccesslog.TcpGrpcAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: logName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: cluster,
						Authority:   hostname,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: filterObjects,
		},
	}

	return &accesslog.AccessLog{
		Name:       TCPEnvoyALSName,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(fl)},
	}
}

func fileAccessLogFromTelemetry(prov *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider) *accesslog.AccessLog {
	p := prov.Path
	if p == "" {
		p = DevStdout
	}
	fl := &fileaccesslog.FileAccessLog{
		Path: p,
	}

	var needsFormatter []*core.TypedExtensionConfig
	if prov.LogFormat != nil {
		switch logFormat := prov.LogFormat.LogFormat.(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Text:
			fl.AccessLogFormat, needsFormatter = buildFileAccessTextLogFormat(logFormat.Text, prov.OmitEmptyValues)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels:
			fl.AccessLogFormat, needsFormatter = buildFileAccessJSONLogFormat(logFormat, prov.OmitEmptyValues)
		}
	} else {
		fl.AccessLogFormat, needsFormatter = buildFileAccessTextLogFormat("", prov.OmitEmptyValues)
	}
	if len(needsFormatter) != 0 {
		fl.GetLogFormat().Formatters = needsFormatter
	}

	al := &accesslog.AccessLog{
		Name:       wellknown.FileAccessLog,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(fl)},
	}

	return al
}

func buildFileAccessTextLogFormat(
	logFormatText string, omitEmptyValues bool,
) (*fileaccesslog.FileAccessLog_LogFormat, []*core.TypedExtensionConfig) {
	formatString := fileAccessLogFormat(logFormatText)
	formatters := accessLogTextFormatters(formatString)

	return &fileaccesslog.FileAccessLog_LogFormat{
		LogFormat: &core.SubstitutionFormatString{
			Format: &core.SubstitutionFormatString_TextFormatSource{
				TextFormatSource: &core.DataSource{
					Specifier: &core.DataSource_InlineString{
						InlineString: formatString,
					},
				},
			},
			OmitEmptyValues: omitEmptyValues,
		},
	}, formatters
}

func buildFileAccessJSONLogFormat(
	logFormat *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels,
	omitEmptyValues bool,
) (*fileaccesslog.FileAccessLog_LogFormat, []*core.TypedExtensionConfig) {
	jsonLogStruct := EnvoyJSONLogFormatIstio
	if logFormat.Labels != nil {
		jsonLogStruct = logFormat.Labels
	}

	// allow default behavior when no labels supplied.
	if len(jsonLogStruct.Fields) == 0 {
		jsonLogStruct = EnvoyJSONLogFormatIstio
	}

	formatters := accessLogJSONFormatters(jsonLogStruct)
	return &fileaccesslog.FileAccessLog_LogFormat{
		LogFormat: &core.SubstitutionFormatString{
			Format: &core.SubstitutionFormatString_JsonFormat{
				JsonFormat: jsonLogStruct,
			},
			JsonFormatOptions: &core.JsonFormatOptions{SortProperties: false},
			OmitEmptyValues:   omitEmptyValues,
		},
	}, formatters
}

func accessLogJSONFormatters(jsonLogStruct *structpb.Struct) []*core.TypedExtensionConfig {
	if jsonLogStruct == nil {
		return nil
	}

	reqWithoutQuery := false
	for _, value := range jsonLogStruct.Fields {
		if !reqWithoutQuery && strings.Contains(value.GetStringValue(), reqWithoutQueryCommandOperator) {
			reqWithoutQuery = true
		}
	}

	formatters := make([]*core.TypedExtensionConfig, 0, maxFormattersLength)
	if reqWithoutQuery {
		formatters = append(formatters, reqWithoutQueryFormatter)
	}

	return formatters
}

func accessLogTextFormatters(text string) []*core.TypedExtensionConfig {
	formatters := make([]*core.TypedExtensionConfig, 0, maxFormattersLength)
	if strings.Contains(text, reqWithoutQueryCommandOperator) {
		formatters = append(formatters, reqWithoutQueryFormatter)
	}

	return formatters
}

func httpGrpcAccessLogFromTelemetry(push *PushContext, proxy *Proxy,
	prov *meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider,
) *accesslog.AccessLog {
	logName := HTTPEnvoyAccessLogFriendlyName
	if prov != nil && prov.LogName != "" {
		logName = prov.LogName
	}

	filterObjects := filterStateObjectsToLog(proxy)
	if len(prov.FilterStateObjectsToLog) != 0 {
		filterObjects = prov.FilterStateObjectsToLog
	}

	hostname, cluster, err := clusterLookupFn(push, prov.Service, int(prov.Port))
	if err != nil {
		IncLookupClusterFailures("envoyHTTPAls")
		log.Errorf("could not find cluster for http grpc provider %q: %v", prov, err)
		return nil
	}

	fl := &grpcaccesslog.HttpGrpcAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: logName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: cluster,
						Authority:   hostname,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: filterObjects,
		},
		AdditionalRequestHeadersToLog:   prov.AdditionalRequestHeadersToLog,
		AdditionalResponseHeadersToLog:  prov.AdditionalResponseHeadersToLog,
		AdditionalResponseTrailersToLog: prov.AdditionalResponseTrailersToLog,
	}

	return &accesslog.AccessLog{
		Name:       wellknown.HTTPGRPCAccessLog,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(fl)},
	}
}

func fileAccessLogFormat(formatString string) string {
	if formatString != "" {
		// From the spec: "NOTE: Istio will insert a newline ('\n') on all formats (if missing)."
		if !strings.HasSuffix(formatString, "\n") {
			formatString += "\n"
		}

		return formatString
	}

	return EnvoyTextLogFormat
}

func FileAccessLogFromMeshConfig(path string, mesh *meshconfig.MeshConfig) *accesslog.AccessLog {
	// We need to build access log. This is needed either on first access or when mesh config changes.
	fl := &fileaccesslog.FileAccessLog{
		Path: path,
	}
	var formatters []*core.TypedExtensionConfig
	switch mesh.AccessLogEncoding {
	case meshconfig.MeshConfig_TEXT:
		formatString := fileAccessLogFormat(mesh.AccessLogFormat)
		formatters = accessLogTextFormatters(formatString)
		fl.AccessLogFormat = &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: formatString,
						},
					},
				},
			},
		}
	case meshconfig.MeshConfig_JSON:
		jsonLogStruct := EnvoyJSONLogFormatIstio
		if len(mesh.AccessLogFormat) > 0 {
			parsedJSONLogStruct := structpb.Struct{}
			if err := protomarshal.UnmarshalAllowUnknown([]byte(mesh.AccessLogFormat), &parsedJSONLogStruct); err != nil {
				log.Errorf("error parsing provided json log format, default log format will be used: %v", err)
			} else {
				jsonLogStruct = &parsedJSONLogStruct
			}
		}
		formatters = accessLogJSONFormatters(jsonLogStruct)
		fl.AccessLogFormat = &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: jsonLogStruct,
				},
				JsonFormatOptions: &core.JsonFormatOptions{SortProperties: false},
			},
		}
	default:
		log.Warnf("unsupported access log format %v", mesh.AccessLogEncoding)
	}

	if len(formatters) > 0 {
		fl.GetLogFormat().Formatters = formatters
	}
	al := &accesslog.AccessLog{
		Name:       wellknown.FileAccessLog,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(fl)},
	}

	return al
}

func openTelemetryLog(pushCtx *PushContext, proxy *Proxy,
	provider *meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider,
) *accesslog.AccessLog {
	hostname, cluster, err := clusterLookupFn(pushCtx, provider.Service, int(provider.Port))
	if err != nil {
		IncLookupClusterFailures("envoyOtelAls")
		log.Errorf("could not find cluster for open telemetry provider %q: %v", provider, err)
		return nil
	}

	logName := provider.LogName
	if logName == "" {
		logName = OtelEnvoyAccessLogFriendlyName
	}

	f := EnvoyTextLogFormat
	if provider.LogFormat != nil && provider.LogFormat.Text != "" {
		f = provider.LogFormat.Text
	}

	var labels *structpb.Struct
	if provider.LogFormat != nil {
		labels = provider.LogFormat.Labels
	}

	cfg := buildOpenTelemetryAccessLogConfig(proxy, logName, hostname, cluster, f, labels)

	return &accesslog.AccessLog{
		Name:       OtelEnvoyALSName,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(cfg)},
	}
}

func buildOpenTelemetryAccessLogConfig(proxy *Proxy,
	logName, hostname, clusterName, format string, labels *structpb.Struct,
) *otelaccesslog.OpenTelemetryAccessLogConfig {
	cfg := &otelaccesslog.OpenTelemetryAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: logName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: clusterName,
						Authority:   hostname,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: filterStateObjectsToLog(proxy),
		},
		DisableBuiltinLabels: true,
	}

	if format != "" {
		cfg.Body = &otlpcommon.AnyValue{
			Value: &otlpcommon.AnyValue_StringValue{
				StringValue: format,
			},
		}
	}

	if labels != nil && len(labels.Fields) != 0 {
		cfg.Attributes = &otlpcommon.KeyValueList{
			Values: ConvertStructToAttributeKeyValues(labels.Fields),
		}
	}

	cfg.Formatters = accessLogFormatters(format, labels)

	return cfg
}

func accessLogFormatters(text string, labels *structpb.Struct) []*core.TypedExtensionConfig {
	formatters := make([]*core.TypedExtensionConfig, 0, maxFormattersLength)
	defer func() {
		slices.SortBy(formatters, func(f *core.TypedExtensionConfig) string {
			return f.Name
		})
	}()

	formatters = append(formatters, accessLogTextFormatters(text)...)
	if len(formatters) >= maxFormattersLength {
		// all formatters are added, return if we have reached the limit
		return formatters
	}

	names := sets.New[string]()
	for _, f := range formatters {
		names.Insert(f.Name)
	}

	for _, f := range accessLogJSONFormatters(labels) {
		if !names.Contains(f.Name) {
			formatters = append(formatters, f)
			names.Insert(f.Name)
		}
	}

	return formatters
}

func ConvertStructToAttributeKeyValues(labels map[string]*structpb.Value) []*otlpcommon.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	attrList := make([]*otlpcommon.KeyValue, 0, len(labels))
	// Sort keys to ensure stable XDS generation
	for _, key := range slices.Sort(maps.Keys(labels)) {
		value := labels[key]
		kv := &otlpcommon.KeyValue{
			Key:   key,
			Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: value.GetStringValue()}},
		}
		attrList = append(attrList, kv)
	}
	return attrList
}

func LookupCluster(push *PushContext, service string, port int) (hostname string, cluster string, err error) {
	if service == "" {
		err = fmt.Errorf("service must not be empty")
		return hostname, cluster, err
	}

	// TODO(yangminzhu): Verify the service and its cluster is supported, e.g. resolution type is not OriginalDst.
	if parts := strings.Split(service, "/"); len(parts) == 2 {
		namespace, name := parts[0], parts[1]
		if svc := push.ServiceIndex.HostnameAndNamespace[host.Name(name)][namespace]; svc != nil {
			hostname = string(svc.Hostname)
			cluster = BuildSubsetKey(TrafficDirectionOutbound, "", svc.Hostname, port)
			return hostname, cluster, err
		}
	} else {
		namespaceToServices := push.ServiceIndex.HostnameAndNamespace[host.Name(service)]
		var namespaces []string
		for k := range namespaceToServices {
			namespaces = append(namespaces, k)
		}
		// If namespace is omitted, return successfully if there is only one such host name in the service index.
		if len(namespaces) == 1 {
			svc := namespaceToServices[namespaces[0]]
			hostname = string(svc.Hostname)
			cluster = BuildSubsetKey(TrafficDirectionOutbound, "", svc.Hostname, port)
			return hostname, cluster, err
		} else if len(namespaces) > 1 {
			err = fmt.Errorf("found %s in multiple namespaces %v, specify the namespace explicitly in "+
				"the format of <Namespace>/<Hostname>", service, namespaces)
			return hostname, cluster, err
		}
	}

	err = fmt.Errorf("could not find service %s in Istio service registry", service)
	return hostname, cluster, err
}
