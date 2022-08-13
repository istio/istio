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
	formatters "github.com/envoyproxy/go-control-plane/envoy/extensions/formatter/req_without_query/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/types/known/structpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/util/protomarshal"
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
		"%UPSTREAM_CLUSTER% %UPSTREAM_LOCAL_ADDRESS% %DOWNSTREAM_LOCAL_ADDRESS% " +
		"%DOWNSTREAM_REMOTE_ADDRESS% %REQUESTED_SERVER_NAME% %ROUTE_NAME%\n"

	HTTPEnvoyAccessLogFriendlyName = "http_envoy_accesslog"
	TCPEnvoyAccessLogFriendlyName  = "tcp_envoy_accesslog"
	OtelEnvoyAccessLogFriendlyName = "otel_envoy_accesslog"

	TCPEnvoyALSName  = "envoy.tcp_grpc_access_log"
	OtelEnvoyALSName = "envoy.access_loggers.open_telemetry"

	requestWithoutQuery = "%REQ_WITHOUT_QUERY"

	DevStdout = "/dev/stdout"

	defaultEnvoyAccessLogProvider = "envoy"
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
			"upstream_cluster":                  {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_CLUSTER%"}},
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
	// But end users can certainly configure it on their own via the meshConfig using the %FILTERSTATE% macro.
	envoyWasmStateToLog = []string{"wasm.upstream_peer", "wasm.upstream_peer_id", "wasm.downstream_peer", "wasm.downstream_peer_id"}

	// accessLogFormatters configures additional formatters needed for some of the format strings like "REQ_WITHOUT_QUERY"
	accessLogFormatters = []*core.TypedExtensionConfig{
		{
			Name:        "envoy.formatter.req_without_query",
			TypedConfig: protoconv.MessageToAny(&formatters.ReqWithoutQuery{}),
		},
	}
)

func telemetryAccessLog(push *PushContext, fp *meshconfig.MeshConfig_ExtensionProvider) *accesslog.AccessLog {
	var al *accesslog.AccessLog
	switch prov := fp.Provider.(type) {
	case *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog:
		// For built-in provider, fallback to Mesh Config for formatting options.
		if fp.Name == defaultEnvoyAccessLogProvider {
			al = FileAccessLogFromMeshConfig(prov.EnvoyFileAccessLog.Path, push.Mesh)
		} else {
			al = fileAccessLogFromTelemetry(prov.EnvoyFileAccessLog)
		}
	case *meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpAls:
		al = httpGrpcAccessLogFromTelemetry(push, prov.EnvoyHttpAls)
	case *meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpAls:
		al = tcpGrpcAccessLogFromTelemetry(push, prov.EnvoyTcpAls)
	case *meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls:
		al = openTelemetryLog(push, prov.EnvoyOtelAls)
	}

	return al
}

func tcpGrpcAccessLogFromTelemetry(push *PushContext, prov *meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider) *accesslog.AccessLog {
	logName := TCPEnvoyAccessLogFriendlyName
	if prov != nil && prov.LogName != "" {
		logName = prov.LogName
	}

	filterObjects := envoyWasmStateToLog
	if len(prov.FilterStateObjectsToLog) != 0 {
		filterObjects = prov.FilterStateObjectsToLog
	}

	hostname, cluster, err := clusterLookupFn(push, prov.Service, int(prov.Port))
	if err != nil {
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

	needsFormatter := false
	if prov.LogFormat != nil {
		switch logFormat := prov.LogFormat.LogFormat.(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Text:
			fl.AccessLogFormat, needsFormatter = buildFileAccessTextLogFormat(logFormat.Text)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels:
			fl.AccessLogFormat, needsFormatter = buildFileAccessJSONLogFormat(logFormat)
		}
	} else {
		fl.AccessLogFormat, needsFormatter = buildFileAccessTextLogFormat("")
	}
	if needsFormatter {
		fl.GetLogFormat().Formatters = accessLogFormatters
	}

	al := &accesslog.AccessLog{
		Name:       wellknown.FileAccessLog,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(fl)},
	}

	return al
}

func buildFileAccessTextLogFormat(logFormatText string) (*fileaccesslog.FileAccessLog_LogFormat, bool) {
	formatString := fileAccessLogFormat(logFormatText)
	needsFormatter := strings.Contains(formatString, requestWithoutQuery)
	return &fileaccesslog.FileAccessLog_LogFormat{
		LogFormat: &core.SubstitutionFormatString{
			Format: &core.SubstitutionFormatString_TextFormatSource{
				TextFormatSource: &core.DataSource{
					Specifier: &core.DataSource_InlineString{
						InlineString: formatString,
					},
				},
			},
		},
	}, needsFormatter
}

func buildFileAccessJSONLogFormat(
	logFormat *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels,
) (*fileaccesslog.FileAccessLog_LogFormat, bool) {
	jsonLogStruct := EnvoyJSONLogFormatIstio
	if logFormat.Labels != nil {
		jsonLogStruct = logFormat.Labels
	}

	// allow default behavior when no labels supplied.
	if len(jsonLogStruct.Fields) == 0 {
		jsonLogStruct = EnvoyJSONLogFormatIstio
	}

	needsFormatter := false
	for _, value := range jsonLogStruct.Fields {
		if strings.Contains(value.GetStringValue(), requestWithoutQuery) {
			needsFormatter = true
			break
		}
	}
	return &fileaccesslog.FileAccessLog_LogFormat{
		LogFormat: &core.SubstitutionFormatString{
			Format: &core.SubstitutionFormatString_JsonFormat{
				JsonFormat: jsonLogStruct,
			},
		},
	}, needsFormatter
}

func httpGrpcAccessLogFromTelemetry(push *PushContext, prov *meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider) *accesslog.AccessLog {
	logName := HTTPEnvoyAccessLogFriendlyName
	if prov != nil && prov.LogName != "" {
		logName = prov.LogName
	}

	filterObjects := envoyWasmStateToLog
	if len(prov.FilterStateObjectsToLog) != 0 {
		filterObjects = prov.FilterStateObjectsToLog
	}

	hostname, cluster, err := clusterLookupFn(push, prov.Service, int(prov.Port))
	if err != nil {
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
	needsFormatter := false
	switch mesh.AccessLogEncoding {
	case meshconfig.MeshConfig_TEXT:
		formatString := fileAccessLogFormat(mesh.AccessLogFormat)
		needsFormatter = strings.Contains(formatString, requestWithoutQuery)
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
		for _, value := range jsonLogStruct.Fields {
			if strings.Contains(value.GetStringValue(), requestWithoutQuery) {
				needsFormatter = true
				break
			}
		}
		fl.AccessLogFormat = &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: jsonLogStruct,
				},
			},
		}
	default:
		log.Warnf("unsupported access log format %v", mesh.AccessLogEncoding)
	}
	if needsFormatter {
		fl.GetLogFormat().Formatters = accessLogFormatters
	}
	al := &accesslog.AccessLog{
		Name:       wellknown.FileAccessLog,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(fl)},
	}

	return al
}

func openTelemetryLog(pushCtx *PushContext,
	provider *meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider,
) *accesslog.AccessLog {
	hostname, cluster, err := clusterLookupFn(pushCtx, provider.Service, int(provider.Port))
	if err != nil {
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

	cfg := buildOpenTelemetryAccessLogConfig(logName, hostname, cluster, f, labels)

	return &accesslog.AccessLog{
		Name:       OtelEnvoyALSName,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(cfg)},
	}
}

func buildOpenTelemetryAccessLogConfig(logName, hostname, clusterName, format string, labels *structpb.Struct) *otelaccesslog.OpenTelemetryAccessLogConfig {
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
			FilterStateObjectsToLog: envoyWasmStateToLog,
		},
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

	return cfg
}

func ConvertStructToAttributeKeyValues(labels map[string]*structpb.Value) []*otlpcommon.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	attrList := make([]*otlpcommon.KeyValue, 0, len(labels))
	for key, value := range labels {
		kv := &otlpcommon.KeyValue{
			Key:   key,
			Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: value.GetStringValue()}},
		}
		attrList = append(attrList, kv)
	}
	return attrList
}

// FIXME: this is a copy of extensionproviders.LookupCluster to avoid import cycle
func LookupCluster(push *PushContext, service string, port int) (hostname string, cluster string, err error) {
	if service == "" {
		err = fmt.Errorf("service must not be empty")
		return
	}

	// TODO(yangminzhu): Verify the service and its cluster is supported, e.g. resolution type is not OriginalDst.
	if parts := strings.Split(service, "/"); len(parts) == 2 {
		namespace, name := parts[0], parts[1]
		if svc := push.ServiceIndex.HostnameAndNamespace[host.Name(name)][namespace]; svc != nil {
			hostname = string(svc.Hostname)
			cluster = BuildSubsetKey(TrafficDirectionOutbound, "", svc.Hostname, port)
			return
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
			return
		} else if len(namespaces) > 1 {
			err = fmt.Errorf("found %s in multiple namespaces %v, specify the namespace explicitly in "+
				"the format of <Namespace>/<Hostname>", service, namespaces)
			return
		}
	}

	err = fmt.Errorf("could not find service %s in Istio service registry", service)
	return
}
