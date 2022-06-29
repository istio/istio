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
	"testing"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	cel "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/filters/cel/v3"
	grpcaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/grpc/v3"
	otelaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/open_telemetry/v3"
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/google/go-cmp/cmp"
	otlpcommon "go.opentelemetry.io/proto/otlp/common/v1"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
)

var (
	httpCodeExpress = "response.code >= 400"
	httpCodeFilter  = &cel.ExpressionFilter{
		Expression: httpCodeExpress,
	}
)

func TestListenerAccessLog(t *testing.T) {
	defaultFormatJSON, _ := protomarshal.ToJSON(EnvoyJSONLogFormatIstio)

	for _, tc := range []struct {
		name       string
		encoding   meshconfig.MeshConfig_AccessLogEncoding
		format     string
		wantFormat string
	}{
		{
			name:       "valid json object",
			encoding:   meshconfig.MeshConfig_JSON,
			format:     `{"foo": "bar"}`,
			wantFormat: `{"foo":"bar"}`,
		},
		{
			name:       "valid nested json object",
			encoding:   meshconfig.MeshConfig_JSON,
			format:     `{"foo": {"bar": "ha"}}`,
			wantFormat: `{"foo":{"bar":"ha"}}`,
		},
		{
			name:       "invalid json object",
			encoding:   meshconfig.MeshConfig_JSON,
			format:     `foo`,
			wantFormat: defaultFormatJSON,
		},
		{
			name:       "incorrect json type",
			encoding:   meshconfig.MeshConfig_JSON,
			format:     `[]`,
			wantFormat: defaultFormatJSON,
		},
		{
			name:       "incorrect json type",
			encoding:   meshconfig.MeshConfig_JSON,
			format:     `"{}"`,
			wantFormat: defaultFormatJSON,
		},
		{
			name:       "default json format",
			encoding:   meshconfig.MeshConfig_JSON,
			wantFormat: defaultFormatJSON,
		},
		{
			name:       "default text format",
			encoding:   meshconfig.MeshConfig_TEXT,
			wantFormat: EnvoyTextLogFormat,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			accessLogBuilder.reset()
			// Update MeshConfig
			m := mesh.DefaultMeshConfig()
			m.AccessLogFile = "foo"
			m.AccessLogEncoding = tc.encoding
			m.AccessLogFormat = tc.format
			listeners := buildListeners(t, TestOptions{MeshConfig: m}, nil)
			if len(listeners) != 2 {
				t.Errorf("expected to have 2 listeners, but got %v", len(listeners))
			}
			// Validate that access log filter uses the new format.
			for _, l := range listeners {
				if l.AccessLog[0].Filter == nil {
					t.Fatal("expected filter config in listener access log configuration")
				}
				// Verify listener access log.
				verify(t, tc.encoding, l.AccessLog[0], tc.wantFormat)

				for _, fc := range l.FilterChains {
					for _, filter := range fc.Filters {
						switch filter.Name {
						case wellknown.TCPProxy:
							tcpConfig := &tcp.TcpProxy{}
							if err := filter.GetTypedConfig().UnmarshalTo(tcpConfig); err != nil {
								t.Fatal(err)
							}
							if tcpConfig.GetCluster() == util.BlackHoleCluster {
								// Ignore the tcp_proxy filter with black hole cluster that just doesn't have access log.
								continue
							}
							if len(tcpConfig.AccessLog) < 1 {
								t.Fatalf("tcp_proxy want at least 1 access log, got 0")
							}

							for _, tcpAccessLog := range tcpConfig.AccessLog {
								if tcpAccessLog.Filter != nil {
									t.Fatalf("tcp_proxy filter chain's accesslog filter must be empty")
								}
							}

							// Verify tcp proxy access log.
							verify(t, tc.encoding, tcpConfig.AccessLog[0], tc.wantFormat)
						case wellknown.HTTPConnectionManager:
							httpConfig := &httppb.HttpConnectionManager{}
							if err := filter.GetTypedConfig().UnmarshalTo(httpConfig); err != nil {
								t.Fatal(err)
							}
							if len(httpConfig.AccessLog) < 1 {
								t.Fatalf("http_connection_manager want at least 1 access log, got 0")
							}
							// Verify HTTP connection manager access log.
							verify(t, tc.encoding, httpConfig.AccessLog[0], tc.wantFormat)
						}
					}
				}
			}
		})
	}
}

func verify(t *testing.T, encoding meshconfig.MeshConfig_AccessLogEncoding, got *accesslog.AccessLog, wantFormat string) {
	cfg, _ := conversion.MessageToStruct(got.GetTypedConfig())
	if encoding == meshconfig.MeshConfig_JSON {
		jsonFormat := cfg.GetFields()["log_format"].GetStructValue().GetFields()["json_format"]
		jsonFormatString, _ := protomarshal.ToJSON(jsonFormat)
		if jsonFormatString != wantFormat {
			t.Errorf("\nwant: %s\n got: %s", wantFormat, jsonFormatString)
		}
	} else {
		textFormatString := cfg.GetFields()["log_format"].GetStructValue().GetFields()["text_format_source"].GetStructValue().
			GetFields()["inline_string"].GetStringValue()
		if textFormatString != wantFormat {
			t.Errorf("\nwant: %s\n got: %s", wantFormat, textFormatString)
		}
	}
}

func TestBuildAccessLogFromTelemetry(t *testing.T) {
	singleCfg := &model.LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: devStdout,
					},
				},
			},
		},
	}

	singleCfgWithFilter := &model.LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: devStdout,
					},
				},
			},
		},
		Filter: &tpb.AccessLogging_Filter{
			Expression: httpCodeExpress,
		},
	}

	customTextFormat := &model.LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: devStdout,
						LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
							LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Text{
								Text: "%LOCAL_REPLY_BODY%:%RESPONSE_CODE%:path=%REQ(:path)%\n",
							},
						},
					},
				},
			},
		},
	}

	defaultJSONFormat := &model.LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: devStdout,
						LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
							LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{
								Labels: &structpb.Struct{},
							},
						},
					},
				},
			},
		},
	}

	customLabelsFormat := &model.LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: devStdout,
						LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
							LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{
								Labels: &structpb.Struct{
									Fields: map[string]*structpb.Value{
										"start_time":                     {Kind: &structpb.Value_StringValue{StringValue: "%START_TIME%"}},
										"route_name":                     {Kind: &structpb.Value_StringValue{StringValue: "%ROUTE_NAME%"}},
										"method":                         {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:METHOD)%"}},
										"path":                           {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"}},
										"protocol":                       {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
										"response_code":                  {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE%"}},
										"response_flags":                 {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_FLAGS%"}},
										"response_code_details":          {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE_DETAILS%"}},
										"connection_termination_details": {Kind: &structpb.Value_StringValue{StringValue: "%CONNECTION_TERMINATION_DETAILS%"}},
										"bytes_received":                 {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_RECEIVED%"}},
										"bytes_sent":                     {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_SENT%"}},
										"duration":                       {Kind: &structpb.Value_StringValue{StringValue: "%DURATION%"}},
										"upstream_service_time":          {Kind: &structpb.Value_StringValue{StringValue: "%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%"}},
										"x_forwarded_for":                {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-FORWARDED-FOR)%"}},
										"user_agent":                     {Kind: &structpb.Value_StringValue{StringValue: "%REQ(USER-AGENT)%"}},
										"request_id":                     {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-REQUEST-ID)%"}},
										"authority":                      {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:AUTHORITY)%"}},
										"upstream_host":                  {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_HOST%"}},
										"upstream_cluster":               {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_CLUSTER%"}},
										"upstream_local_address":         {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_LOCAL_ADDRESS%"}},
										"downstream_local_address":       {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_LOCAL_ADDRESS%"}},
										"downstream_remote_address":      {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_REMOTE_ADDRESS%"}},
										"requested_server_name":          {Kind: &structpb.Value_StringValue{StringValue: "%REQUESTED_SERVER_NAME%"}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	multiCfg := &model.LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "stdout",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: devStdout,
					},
				},
			},
			{
				Name: "stderr",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: "/dev/stderr",
					},
				},
			},
		},
	}

	fakeFilterStateObjects := []string{"fake-filter-state-object1", "fake-filter-state-object1"}
	grpcCfg := &model.LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "stdout",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: devStdout,
					},
				},
			},
			{
				Name: "grpc-http-als",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpAls{
					EnvoyHttpAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyHttpGrpcV3LogProvider{
						LogName:                         "grpc-http-als",
						Service:                         "grpc-als.foo.svc.cluster.local",
						Port:                            9811,
						AdditionalRequestHeadersToLog:   []string{"fake-request-header1"},
						AdditionalResponseHeadersToLog:  []string{"fake-response-header1"},
						AdditionalResponseTrailersToLog: []string{"fake-response-trailer1"},
						FilterStateObjectsToLog:         fakeFilterStateObjects,
					},
				},
			},
		},
	}

	grpcTCPCfg := &model.LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "stdout",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: devStdout,
					},
				},
			},
			{
				Name: "grpc-tcp-als",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpAls{
					EnvoyTcpAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyTcpGrpcV3LogProvider{
						LogName:                 "grpc-tcp-als",
						Service:                 "grpc-als.foo.svc.cluster.local",
						Port:                    9811,
						FilterStateObjectsToLog: fakeFilterStateObjects,
					},
				},
			},
		},
	}

	labels := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"protocol": {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
		},
	}

	multiWithOtelCfg := &model.LoggingConfig{
		Providers: []*meshconfig.MeshConfig_ExtensionProvider{
			{
				Name: "stdout",
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
					EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
						Path: devStdout,
					},
				},
			},
			{
				Name: otelEnvoyAccessLogFriendlyName,
				Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOtelAls{
					EnvoyOtelAls: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider{
						Service: "otel.foo.svc.cluster.local",
						Port:    9811,
						LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyOpenTelemetryLogProvider_LogFormat{
							Labels: labels,
						},
					},
				},
			},
		},
	}

	grpcBackendClusterName := "outbound|9811||grpc-als.foo.svc.cluster.local"
	grpcBackendAuthority := "grpc-als.foo.svc.cluster.local"
	otelCfg := &otelaccesslog.OpenTelemetryAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: otelEnvoyAccessLogFriendlyName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: grpcBackendClusterName,
						Authority:   grpcBackendAuthority,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: envoyWasmStateToLog,
		},
		Body: &otlpcommon.AnyValue{
			Value: &otlpcommon.AnyValue_StringValue{
				StringValue: EnvoyTextLogFormat,
			},
		},
		Attributes: &otlpcommon.KeyValueList{
			Values: convertStructToAttributeKeyValues(labels.Fields),
		},
	}

	clusterLookupFn = func(push *model.PushContext, service string, port int) (hostname string, cluster string, err error) {
		return grpcBackendAuthority, grpcBackendClusterName, nil
	}

	stdout := &fileaccesslog.FileAccessLog{
		Path: devStdout,
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: EnvoyTextLogFormat,
						},
					},
				},
			},
		},
	}

	customTextOut := &fileaccesslog.FileAccessLog{
		Path: devStdout,
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: "%LOCAL_REPLY_BODY%:%RESPONSE_CODE%:path=%REQ(:path)%\n",
						},
					},
				},
			},
		},
	}

	defaultJSONLabelsOut := &fileaccesslog.FileAccessLog{
		Path: devStdout,
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: EnvoyJSONLogFormatIstio,
				},
			},
		},
	}

	customLabelsOut := &fileaccesslog.FileAccessLog{
		Path: devStdout,
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: &structpb.Struct{
						Fields: map[string]*structpb.Value{
							"start_time":                     {Kind: &structpb.Value_StringValue{StringValue: "%START_TIME%"}},
							"route_name":                     {Kind: &structpb.Value_StringValue{StringValue: "%ROUTE_NAME%"}},
							"method":                         {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:METHOD)%"}},
							"path":                           {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-ENVOY-ORIGINAL-PATH?:PATH)%"}},
							"protocol":                       {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
							"response_code":                  {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE%"}},
							"response_flags":                 {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_FLAGS%"}},
							"response_code_details":          {Kind: &structpb.Value_StringValue{StringValue: "%RESPONSE_CODE_DETAILS%"}},
							"connection_termination_details": {Kind: &structpb.Value_StringValue{StringValue: "%CONNECTION_TERMINATION_DETAILS%"}},
							"bytes_received":                 {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_RECEIVED%"}},
							"bytes_sent":                     {Kind: &structpb.Value_StringValue{StringValue: "%BYTES_SENT%"}},
							"duration":                       {Kind: &structpb.Value_StringValue{StringValue: "%DURATION%"}},
							"upstream_service_time":          {Kind: &structpb.Value_StringValue{StringValue: "%RESP(X-ENVOY-UPSTREAM-SERVICE-TIME)%"}},
							"x_forwarded_for":                {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-FORWARDED-FOR)%"}},
							"user_agent":                     {Kind: &structpb.Value_StringValue{StringValue: "%REQ(USER-AGENT)%"}},
							"request_id":                     {Kind: &structpb.Value_StringValue{StringValue: "%REQ(X-REQUEST-ID)%"}},
							"authority":                      {Kind: &structpb.Value_StringValue{StringValue: "%REQ(:AUTHORITY)%"}},
							"upstream_host":                  {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_HOST%"}},
							"upstream_cluster":               {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_CLUSTER%"}},
							"upstream_local_address":         {Kind: &structpb.Value_StringValue{StringValue: "%UPSTREAM_LOCAL_ADDRESS%"}},
							"downstream_local_address":       {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_LOCAL_ADDRESS%"}},
							"downstream_remote_address":      {Kind: &structpb.Value_StringValue{StringValue: "%DOWNSTREAM_REMOTE_ADDRESS%"}},
							"requested_server_name":          {Kind: &structpb.Value_StringValue{StringValue: "%REQUESTED_SERVER_NAME%"}},
						},
					},
				},
			},
		},
	}

	errout := &fileaccesslog.FileAccessLog{
		Path: "/dev/stderr",
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: EnvoyTextLogFormat,
						},
					},
				},
			},
		},
	}

	grpcHTTPout := &grpcaccesslog.HttpGrpcAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: "grpc-http-als",
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: grpcBackendClusterName,
						Authority:   grpcBackendAuthority,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: fakeFilterStateObjects,
		},
		AdditionalRequestHeadersToLog:   []string{"fake-request-header1"},
		AdditionalResponseHeadersToLog:  []string{"fake-response-header1"},
		AdditionalResponseTrailersToLog: []string{"fake-response-trailer1"},
	}

	grpcTCPOut := &grpcaccesslog.TcpGrpcAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: "grpc-tcp-als",
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: grpcBackendClusterName,
						Authority:   grpcBackendAuthority,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: fakeFilterStateObjects,
		},
	}

	ctx := model.NewPushContext()
	ctx.ServiceIndex.HostnameAndNamespace["otel-collector.foo.svc.cluster.local"] = map[string]*model.Service{
		"foo": {
			Hostname:       "otel-collector.foo.svc.cluster.local",
			DefaultAddress: "172.217.0.0/16",
			Ports: model.PortList{
				&model.Port{
					Name:     "grpc-port",
					Port:     3417,
					Protocol: protocol.TCP,
				},
				&model.Port{
					Name:     "http-port",
					Port:     3418,
					Protocol: protocol.HTTP,
				},
			},
			Resolution: model.ClientSideLB,
			Attributes: model.ServiceAttributes{
				Name:            "otel-collector",
				Namespace:       "foo",
				ServiceRegistry: provider.Kubernetes,
			},
		},
	}

	for _, tc := range []struct {
		name        string
		ctx         *model.PushContext
		meshConfig  *meshconfig.MeshConfig
		spec        *model.LoggingConfig
		forListener bool

		expected []*accesslog.AccessLog
	}{
		{
			name: "single",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec:        singleCfg,
			forListener: false,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(stdout)},
				},
			},
		},
		{
			name: "with-filter",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec:        singleCfgWithFilter,
			forListener: false,
			expected: []*accesslog.AccessLog{
				{
					Name: wellknown.FileAccessLog,
					Filter: &accesslog.AccessLogFilter{
						FilterSpecifier: &accesslog.AccessLogFilter_ExtensionFilter{
							ExtensionFilter: &accesslog.ExtensionFilter{
								Name:       celFilter,
								ConfigType: &accesslog.ExtensionFilter_TypedConfig{TypedConfig: util.MessageToAny(httpCodeFilter)},
							},
						},
					},
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(stdout)},
				},
			},
		},
		{
			name: "tcp-with-filter",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec:        singleCfgWithFilter,
			forListener: true,
			expected: []*accesslog.AccessLog{
				{
					Name: wellknown.FileAccessLog,
					Filter: &accesslog.AccessLogFilter{
						FilterSpecifier: &accesslog.AccessLogFilter_AndFilter{
							AndFilter: &accesslog.AndFilter{
								Filters: []*accesslog.AccessLogFilter{
									{
										FilterSpecifier: &accesslog.AccessLogFilter_ResponseFlagFilter{
											ResponseFlagFilter: &accesslog.ResponseFlagFilter{Flags: []string{"NR"}},
										},
									},
									{
										FilterSpecifier: &accesslog.AccessLogFilter_ExtensionFilter{
											ExtensionFilter: &accesslog.ExtensionFilter{
												Name:       celFilter,
												ConfigType: &accesslog.ExtensionFilter_TypedConfig{TypedConfig: util.MessageToAny(httpCodeFilter)},
											},
										},
									},
								},
							},
						},
					},
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(stdout)},
				},
			},
		},
		{
			name: "custom-text",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec:        customTextFormat,
			forListener: false,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(customTextOut)},
				},
			},
		},
		{
			name: "default-labels",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec:        defaultJSONFormat,
			forListener: false,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(defaultJSONLabelsOut)},
				},
			},
		},
		{
			name: "custom-labels",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec:        customLabelsFormat,
			forListener: false,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(customLabelsOut)},
				},
			},
		},
		{
			name: "single-listener",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec:        singleCfg,
			forListener: true,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					Filter:     addAccessLogFilter(),
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(stdout)},
				},
			},
		},
		{
			name: "multi",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec:        multiCfg,
			forListener: false,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(stdout)},
				},
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(errout)},
				},
			},
		},
		{
			name: "multi-listener",
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec:        multiCfg,
			forListener: true,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					Filter:     addAccessLogFilter(),
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(stdout)},
				},
				{
					Name:       wellknown.FileAccessLog,
					Filter:     addAccessLogFilter(),
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(errout)},
				},
			},
		},
		{
			name: "grpc-als",
			spec: grpcCfg,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			forListener: false,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(stdout)},
				},
				{
					Name:       wellknown.HTTPGRPCAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(grpcHTTPout)},
				},
			},
		},
		{
			name: "grpc-tcp-als",
			spec: grpcTCPCfg,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			forListener: false,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(stdout)},
				},
				{
					Name:       tcpEnvoyALSName,
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(grpcTCPOut)},
				},
			},
		},
		{
			name: "multi-with-open-telemetry",
			ctx:  ctx,
			meshConfig: &meshconfig.MeshConfig{
				AccessLogEncoding: meshconfig.MeshConfig_TEXT,
			},
			spec:        multiWithOtelCfg,
			forListener: true,
			expected: []*accesslog.AccessLog{
				{
					Name:       wellknown.FileAccessLog,
					Filter:     addAccessLogFilter(),
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(stdout)},
				},
				{
					Name:       otelEnvoyALSName,
					Filter:     addAccessLogFilter(),
					ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: util.MessageToAny(otelCfg)},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildAccessLogFromTelemetry(tc.ctx, tc.spec, tc.forListener)

			assert.Equal(t, tc.expected, got)
		})
	}
}

func TestAccessLogPatch(t *testing.T) {
	// Regression test for https://github.com/istio/istio/issues/35778
	cg := NewConfigGenTest(t, TestOptions{
		Configs:        nil,
		ConfigPointers: nil,
		ConfigString: `
apiVersion: networking.istio.io/v1alpha3
kind: EnvoyFilter
metadata:
  name: access-log-format
  namespace: default
spec:
  configPatches:
  - applyTo: NETWORK_FILTER
    match:
      context: ANY
      listener:
        filterChain:
          filter:
            name: envoy.filters.network.tcp_proxy
    patch:
      operation: MERGE
      value:
        typed_config:
          '@type': type.googleapis.com/envoy.extensions.filters.network.tcp_proxy.v3.TcpProxy
          access_log:
          - name: envoy.access_loggers.stream
            typed_config:
              '@type': type.googleapis.com/envoy.extensions.access_loggers.stream.v3.StdoutAccessLog
              log_format:
                json_format:
                  envoyproxy_authority: '%REQ(:AUTHORITY)%'
`,
	})

	proxy := cg.SetupProxy(nil)
	l1 := cg.Listeners(proxy)
	l2 := cg.Listeners(proxy)
	// Make sure it doesn't change between patches
	if d := cmp.Diff(l1, l2, protocmp.Transform()); d != "" {
		t.Fatal(d)
	}
	// Make sure we have exactly 1 access log
	fc := xdstest.ExtractFilterChain("virtualOutbound-blackhole", xdstest.ExtractListener("virtualOutbound", l1))
	if len(xdstest.ExtractTCPProxy(t, fc).GetAccessLog()) != 1 {
		t.Fatalf("unexpected access log: %v", xdstest.ExtractTCPProxy(t, fc).GetAccessLog())
	}
}

func TestBuildOpenTelemetryAccessLogConfig(t *testing.T) {
	fakeCluster := "outbound|55680||otel-collector.monitoring.svc.cluster.local"
	fakeAuthority := "otel-collector.monitoring.svc.cluster.local"
	for _, tc := range []struct {
		name        string
		logName     string
		clusterName string
		hostname    string
		body        string
		labels      *structpb.Struct
		expected    *otelaccesslog.OpenTelemetryAccessLogConfig
	}{
		{
			name:        "default",
			logName:     otelEnvoyAccessLogFriendlyName,
			clusterName: fakeCluster,
			hostname:    fakeAuthority,
			body:        EnvoyTextLogFormat,
			expected: &otelaccesslog.OpenTelemetryAccessLogConfig{
				CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
					LogName: otelEnvoyAccessLogFriendlyName,
					GrpcService: &core.GrpcService{
						TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
								ClusterName: fakeCluster,
								Authority:   fakeAuthority,
							},
						},
					},
					TransportApiVersion:     core.ApiVersion_V3,
					FilterStateObjectsToLog: envoyWasmStateToLog,
				},
				Body: &otlpcommon.AnyValue{
					Value: &otlpcommon.AnyValue_StringValue{
						StringValue: EnvoyTextLogFormat,
					},
				},
			},
		},
		{
			name:        "with attrs",
			logName:     otelEnvoyAccessLogFriendlyName,
			clusterName: fakeCluster,
			hostname:    fakeAuthority,
			body:        EnvoyTextLogFormat,
			labels: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"protocol": {Kind: &structpb.Value_StringValue{StringValue: "%PROTOCOL%"}},
				},
			},
			expected: &otelaccesslog.OpenTelemetryAccessLogConfig{
				CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
					LogName: otelEnvoyAccessLogFriendlyName,
					GrpcService: &core.GrpcService{
						TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
							EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
								ClusterName: fakeCluster,
								Authority:   fakeAuthority,
							},
						},
					},
					TransportApiVersion:     core.ApiVersion_V3,
					FilterStateObjectsToLog: envoyWasmStateToLog,
				},
				Body: &otlpcommon.AnyValue{
					Value: &otlpcommon.AnyValue_StringValue{
						StringValue: EnvoyTextLogFormat,
					},
				},
				Attributes: &otlpcommon.KeyValueList{
					Values: []*otlpcommon.KeyValue{
						{
							Key:   "protocol",
							Value: &otlpcommon.AnyValue{Value: &otlpcommon.AnyValue_StringValue{StringValue: "%PROTOCOL%"}},
						},
					},
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildOpenTelemetryAccessLogConfig(tc.logName, tc.hostname, tc.clusterName, tc.body, tc.labels)
			assert.Equal(t, tc.expected, got)
		})
	}
}
