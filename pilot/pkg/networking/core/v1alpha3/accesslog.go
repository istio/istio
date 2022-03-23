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
	"sync"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	cel "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/filters/cel/v3"
	grpcaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/grpc/v3"
	otelaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/open_telemetry/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	formatters "github.com/envoyproxy/go-control-plane/envoy/extensions/formatter/req_without_query/v3"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/types/known/structpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/pkg/log"
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

	// EnvoyServerName for istio's envoy
	EnvoyServerName = "istio-envoy"

	httpEnvoyAccessLogFriendlyName     = "http_envoy_accesslog"
	tcpEnvoyAccessLogFriendlyName      = "tcp_envoy_accesslog"
	otelEnvoyAccessLogFriendlyName     = "otel_envoy_accesslog"
	listenerEnvoyAccessLogFriendlyName = "listener_envoy_accesslog"

	tcpEnvoyALSName  = "envoy.tcp_grpc_access_log"
	otelEnvoyALSName = "envoy.access_loggers.open_telemetry"

	// EnvoyAccessLogCluster is the cluster name that has details for server implementing Envoy ALS.
	// This cluster is created in bootstrap.
	EnvoyAccessLogCluster = "envoy_accesslog_service"

	requestWithoutQuery = "%REQ_WITHOUT_QUERY"

	devStdout = "/dev/stdout"

	celFilter = "envoy.access_loggers.extension_filters.cel"
)

var (
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

	// accessLogBuilder is used to set accessLog to filters
	accessLogBuilder = newAccessLogBuilder()

	// accessLogFormatters configures additional formatters needed for some of the format strings like "REQ_WITHOUT_QUERY"
	accessLogFormatters = []*core.TypedExtensionConfig{
		{
			Name:        "envoy.formatter.req_without_query",
			TypedConfig: util.MessageToAny(&formatters.ReqWithoutQuery{}),
		},
	}
)

type AccessLogBuilder struct {
	// tcpGrpcAccessLog is used when access log service is enabled in mesh config.
	tcpGrpcAccessLog *accesslog.AccessLog
	// httpGrpcAccessLog is used when access log service is enabled in mesh config.
	httpGrpcAccessLog *accesslog.AccessLog
	// tcpGrpcListenerAccessLog is used when access log service is enabled in mesh config.
	tcpGrpcListenerAccessLog *accesslog.AccessLog

	// file accessLog which is cached and reset on MeshConfig change.
	mutex                 sync.RWMutex
	fileAccesslog         *accesslog.AccessLog
	listenerFileAccessLog *accesslog.AccessLog
}

func newAccessLogBuilder() *AccessLogBuilder {
	return &AccessLogBuilder{
		tcpGrpcAccessLog:         buildTCPGrpcAccessLog(false),
		httpGrpcAccessLog:        buildHTTPGrpcAccessLog(),
		tcpGrpcListenerAccessLog: buildTCPGrpcAccessLog(true),
	}
}

func (b *AccessLogBuilder) setTCPAccessLog(push *model.PushContext, proxy *model.Proxy, tcp *tcp.TcpProxy) {
	mesh := push.Mesh
	cfg := push.Telemetry.AccessLogging(proxy)

	if cfg == nil {
		// No Telemetry API configured, fall back to legacy mesh config setting
		if mesh.AccessLogFile != "" {
			tcp.AccessLog = append(tcp.AccessLog, b.buildFileAccessLog(mesh))
		}

		if mesh.EnableEnvoyAccessLogService {
			// Setting it to TCP as the low level one.
			tcp.AccessLog = append(tcp.AccessLog, b.tcpGrpcAccessLog)
		}
		return
	}

	if al := buildAccessLogFromTelemetry(push, cfg, false); len(al) != 0 {
		tcp.AccessLog = append(tcp.AccessLog, al...)
	}
}

func buildAccessLogFromTelemetry(push *model.PushContext, spec *model.LoggingConfig, forListener bool) []*accesslog.AccessLog {
	als := make([]*accesslog.AccessLog, 0)
	telFilter := buildAccessLogFilterFromTelemetry(spec)
	filters := []*accesslog.AccessLogFilter{}
	if forListener {
		filters = append(filters, addAccessLogFilter())
	}
	if telFilter != nil {
		filters = append(filters, telFilter)
	}

	for _, p := range spec.Providers {
		var al *accesslog.AccessLog
		switch prov := p.Provider.(type) {
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog:
			al = buildEnvoyFileAccessLogHelper(prov.EnvoyFileAccessLog)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpAls:
			al = buildHTTPGrpcAccessLogHelper(push, prov.EnvoyHttpAls)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpAls:
			al = buildTCPGrpcAccessLogHelper(push, prov.EnvoyTcpAls)
		case *meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls:
			al = buildOpenTelemetryLogHelper(push, prov.EnvoyOtelAls)
		}
		if al == nil {
			continue
		}

		al.Filter = buildAccessLogFilter(filters...)
		als = append(als, al)
	}
	return als
}

func buildAccessLogFilterFromTelemetry(spec *model.LoggingConfig) *accesslog.AccessLogFilter {
	if spec == nil || spec.Filter == nil {
		return nil
	}

	fl := &cel.ExpressionFilter{
		Expression: spec.Filter.Expression,
	}

	return &accesslog.AccessLogFilter{
		FilterSpecifier: &accesslog.AccessLogFilter_ExtensionFilter{
			ExtensionFilter: &accesslog.ExtensionFilter{
				Name:       celFilter,
				ConfigType: &accesslog.ExtensionFilter_TypedConfig{TypedConfig: util.MessageToAny(fl)},
			},
		},
	}
}

func (b *AccessLogBuilder) setHTTPAccessLog(opts buildListenerOpts, connectionManager *hcm.HttpConnectionManager) {
	mesh := opts.push.Mesh
	cfg := opts.push.Telemetry.AccessLogging(opts.proxy)

	if cfg == nil {
		// No Telemetry API configured, fall back to legacy mesh config setting
		if mesh.AccessLogFile != "" {
			connectionManager.AccessLog = append(connectionManager.AccessLog, b.buildFileAccessLog(mesh))
		}

		if mesh.EnableEnvoyAccessLogService {
			connectionManager.AccessLog = append(connectionManager.AccessLog, b.httpGrpcAccessLog)
		}
		return
	}

	if al := buildAccessLogFromTelemetry(opts.push, cfg, false); len(al) != 0 {
		connectionManager.AccessLog = append(connectionManager.AccessLog, al...)
	}
}

func (b *AccessLogBuilder) setListenerAccessLog(push *model.PushContext, proxy *model.Proxy, listener *listener.Listener) {
	mesh := push.Mesh
	if mesh.DisableEnvoyListenerLog {
		return
	}
	cfg := push.Telemetry.AccessLogging(proxy)

	if cfg == nil {
		// No Telemetry API configured, fall back to legacy mesh config setting
		if mesh.AccessLogFile != "" {
			listener.AccessLog = append(listener.AccessLog, b.buildListenerFileAccessLog(mesh))
		}

		if mesh.EnableEnvoyAccessLogService {
			// Setting it to TCP as the low level one.
			listener.AccessLog = append(listener.AccessLog, b.tcpGrpcListenerAccessLog)
		}
		return
	}

	if al := buildAccessLogFromTelemetry(push, cfg, true); len(al) != 0 {
		listener.AccessLog = append(listener.AccessLog, al...)
	}
}

func buildTCPGrpcAccessLogHelper(push *model.PushContext, prov *meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider) *accesslog.AccessLog {
	logName := tcpEnvoyAccessLogFriendlyName
	if prov != nil && prov.LogName != "" {
		logName = prov.LogName
	}

	filterObjects := envoyWasmStateToLog
	if len(prov.FilterStateObjectsToLog) != 0 {
		filterObjects = prov.FilterStateObjectsToLog
	}

	_, cluster, err := clusterLookupFn(push, prov.Service, int(prov.Port))
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
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: filterObjects,
		},
	}

	return &accesslog.AccessLog{
		Name:       tcpEnvoyALSName,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(fl)},
	}
}

func buildEnvoyFileAccessLogHelper(prov *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider) *accesslog.AccessLog {
	p := prov.Path
	if p == "" {
		p = devStdout
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
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(fl)},
	}

	return al
}

func buildFileAccessTextLogFormat(text string) (*fileaccesslog.FileAccessLog_LogFormat, bool) {
	formatString := EnvoyTextLogFormat
	if text != "" {
		formatString = text
	}
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
	logFormat *meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels) (*fileaccesslog.FileAccessLog_LogFormat, bool) {
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
		if value.GetStringValue() == requestWithoutQuery {
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

func buildHTTPGrpcAccessLogHelper(push *model.PushContext, prov *meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider) *accesslog.AccessLog {
	logName := httpEnvoyAccessLogFriendlyName
	if prov != nil && prov.LogName != "" {
		logName = prov.LogName
	}

	filterObjects := envoyWasmStateToLog
	if len(prov.FilterStateObjectsToLog) != 0 {
		filterObjects = prov.FilterStateObjectsToLog
	}

	_, cluster, err := clusterLookupFn(push, prov.Service, int(prov.Port))
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
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(fl)},
	}
}

func buildFileAccessLogHelper(path string, mesh *meshconfig.MeshConfig) *accesslog.AccessLog {
	// We need to build access log. This is needed either on first access or when mesh config changes.
	fl := &fileaccesslog.FileAccessLog{
		Path: path,
	}
	needsFormatter := false
	switch mesh.AccessLogEncoding {
	case meshconfig.MeshConfig_TEXT:
		formatString := EnvoyTextLogFormat
		if mesh.AccessLogFormat != "" {
			formatString = mesh.AccessLogFormat
		}
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
			if value.GetStringValue() == requestWithoutQuery {
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
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(fl)},
	}

	return al
}

func buildOpenTelemetryLogHelper(pushCtx *model.PushContext,
	provider *meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider) *accesslog.AccessLog {
	_, cluster, err := clusterLookupFn(pushCtx, provider.Service, int(provider.Port))
	if err != nil {
		log.Errorf("could not find cluster for open telemetry provider %q: %v", provider, err)
		return nil
	}

	logName := provider.LogName
	if logName == "" {
		logName = otelEnvoyAccessLogFriendlyName
	}

	f := EnvoyTextLogFormat
	if provider.LogFormat != nil && provider.LogFormat.Text != "" {
		f = provider.LogFormat.Text
	}

	var labels *structpb.Struct
	if provider.LogFormat != nil {
		labels = provider.LogFormat.Labels
	}

	cfg := buildOpenTelemetryAccessLogConfig(logName, cluster, f, labels)

	return &accesslog.AccessLog{
		Name:       otelEnvoyALSName,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(cfg)},
	}
}

func buildOpenTelemetryAccessLogConfig(logName, clusterName, format string, labels *structpb.Struct) *otelaccesslog.OpenTelemetryAccessLogConfig {
	cfg := &otelaccesslog.OpenTelemetryAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: logName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: clusterName,
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
			Values: convertStructToAttributeKeyValues(labels.Fields),
		}
	}

	return cfg
}

func convertStructToAttributeKeyValues(labels map[string]*structpb.Value) []*otlpcommon.KeyValue {
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

func (b *AccessLogBuilder) buildFileAccessLog(mesh *meshconfig.MeshConfig) *accesslog.AccessLog {
	if cal := b.cachedFileAccessLog(); cal != nil {
		return cal
	}

	// We need to build access log. This is needed either on first access or when mesh config changes.
	al := buildFileAccessLogHelper(mesh.AccessLogFile, mesh)

	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.fileAccesslog = al

	return al
}

func addAccessLogFilter() *accesslog.AccessLogFilter {
	return &accesslog.AccessLogFilter{
		FilterSpecifier: &accesslog.AccessLogFilter_ResponseFlagFilter{
			ResponseFlagFilter: &accesslog.ResponseFlagFilter{Flags: []string{"NR"}},
		},
	}
}

func buildAccessLogFilter(f ...*accesslog.AccessLogFilter) *accesslog.AccessLogFilter {
	if len(f) == 0 {
		return nil
	}

	if len(f) == 1 {
		return f[0]
	}

	return &accesslog.AccessLogFilter{
		FilterSpecifier: &accesslog.AccessLogFilter_AndFilter{
			AndFilter: &accesslog.AndFilter{
				Filters: f,
			},
		},
	}
}

func (b *AccessLogBuilder) buildListenerFileAccessLog(mesh *meshconfig.MeshConfig) *accesslog.AccessLog {
	if cal := b.cachedListenerFileAccessLog(); cal != nil {
		return cal
	}

	// We need to build access log. This is needed either on first access or when mesh config changes.
	lal := buildFileAccessLogHelper(mesh.AccessLogFile, mesh)
	// We add ResponseFlagFilter here, as we want to get listener access logs only on scenarios where we might
	// not get filter Access Logs like in cases like NR to upstream.
	lal.Filter = addAccessLogFilter()

	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.listenerFileAccessLog = lal

	return lal
}

func (b *AccessLogBuilder) cachedFileAccessLog() *accesslog.AccessLog {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.fileAccesslog
}

func (b *AccessLogBuilder) cachedListenerFileAccessLog() *accesslog.AccessLog {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.listenerFileAccessLog
}

func buildTCPGrpcAccessLog(isListener bool) *accesslog.AccessLog {
	accessLogFriendlyName := tcpEnvoyAccessLogFriendlyName
	if isListener {
		accessLogFriendlyName = listenerEnvoyAccessLogFriendlyName
	}
	fl := &grpcaccesslog.TcpGrpcAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: accessLogFriendlyName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: EnvoyAccessLogCluster,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: envoyWasmStateToLog,
		},
	}

	var filter *accesslog.AccessLogFilter
	if isListener {
		filter = addAccessLogFilter()
	}
	return &accesslog.AccessLog{
		Name:       tcpEnvoyALSName,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(fl)},
		Filter:     filter,
	}
}

func buildHTTPGrpcAccessLog() *accesslog.AccessLog {
	fl := &grpcaccesslog.HttpGrpcAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: httpEnvoyAccessLogFriendlyName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: EnvoyAccessLogCluster,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: envoyWasmStateToLog,
		},
	}

	return &accesslog.AccessLog{
		Name:       wellknown.HTTPGRPCAccessLog,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(fl)},
	}
}

func (b *AccessLogBuilder) reset() {
	b.mutex.Lock()
	b.fileAccesslog = nil
	b.listenerFileAccessLog = nil
	b.mutex.Unlock()
}
