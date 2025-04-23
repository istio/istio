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
	"testing"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	fileaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/file/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/wrapperspb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	tpb "istio.io/api/telemetry/v1alpha1"
	"istio.io/api/type/v1beta1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/networking/util"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/wellknown"
)

func TestListenerAccessLog(t *testing.T) {
	defaultFormatJSON, _ := protomarshal.ToJSON(model.EnvoyJSONLogFormatIstio)

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
			wantFormat: model.EnvoyTextLogFormat,
		},
	} {
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
								t.Fatal("tcp_proxy want at least 1 access log, got 0")
							}

							for _, tcpAccessLog := range tcpConfig.AccessLog {
								if tcpAccessLog.Filter != nil {
									t.Fatal("tcp_proxy filter chain's accesslog filter must be empty")
								}
							}

							// Verify tcp proxy access log.
							verify(t, tc.encoding, tcpConfig.AccessLog[0], tc.wantFormat)
						case wellknown.HTTPConnectionManager:
							httpConfig := &hcm.HttpConnectionManager{}
							if err := filter.GetTypedConfig().UnmarshalTo(httpConfig); err != nil {
								t.Fatal(err)
							}
							if len(httpConfig.AccessLog) < 1 {
								t.Fatal("http_connection_manager want at least 1 access log, got 0")
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
	cfg, _ := protomarshal.MessageToStructSlow(got.GetTypedConfig())
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

func newTestEnvironment() *model.Environment {
	serviceDiscovery := memregistry.NewServiceDiscovery(&model.Service{
		Hostname:       "test.example.org",
		DefaultAddress: "1.1.1.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "default",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
	})

	meshConfig := mesh.DefaultMeshConfig()
	meshConfig.AccessLogFile = model.DevStdout
	meshConfig.ExtensionProviders = append(meshConfig.ExtensionProviders, &meshconfig.MeshConfig_ExtensionProvider{
		Name: "envoy-json",
		Provider: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLog{
			EnvoyFileAccessLog: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider{
				Path: model.DevStdout,
				LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat{
					LogFormat: &meshconfig.MeshConfig_ExtensionProvider_EnvoyFileAccessLogProvider_LogFormat_Labels{},
				},
			},
		},
	})

	configStore := memory.Make(collections.Pilot)
	configStore.Create(config.Config{
		Meta: config.Meta{
			Name:             "test",
			Namespace:        "default",
			GroupVersionKind: gvk.Telemetry,
		},
		Spec: &tpb.Telemetry{
			Selector: &v1beta1.WorkloadSelector{
				MatchLabels: map[string]string{
					"app": "test",
				},
			},
			AccessLogging: []*tpb.AccessLogging{
				{
					Providers: []*tpb.ProviderRef{
						{
							Name: "envoy-json",
						},
					},
				},
			},
		},
	})
	configStore.Create(config.Config{
		Meta: config.Meta{
			Name:             "test-with-server-accesslog-filter",
			Namespace:        "default",
			GroupVersionKind: gvk.Telemetry,
		},
		Spec: &tpb.Telemetry{
			Selector: &v1beta1.WorkloadSelector{
				MatchLabels: map[string]string{
					"app": "test-with-server-accesslog-filter",
				},
			},
			AccessLogging: []*tpb.AccessLogging{
				{
					Match: &tpb.AccessLogging_LogSelector{
						Mode: tpb.WorkloadMode_SERVER,
					},
					Providers: []*tpb.ProviderRef{
						{
							Name: "envoy-json",
						},
					},
				},
			},
		},
	})
	configStore.Create(config.Config{
		Meta: config.Meta{
			Name:             "test-disable-accesslog",
			Namespace:        "default",
			GroupVersionKind: gvk.Telemetry,
		},
		Spec: &tpb.Telemetry{
			Selector: &v1beta1.WorkloadSelector{
				MatchLabels: map[string]string{
					"app": "test-disable-accesslog",
				},
			},
			AccessLogging: []*tpb.AccessLogging{
				{
					Providers: []*tpb.ProviderRef{
						{
							Name: "envoy",
						},
					},
					Disabled: wrapperspb.Bool(true),
				},
			},
		},
	})

	env := model.NewEnvironment()
	env.ServiceDiscovery = serviceDiscovery
	env.ConfigStore = configStore
	env.Watcher = meshwatcher.NewTestWatcher(meshConfig)

	pushContext := model.NewPushContext()
	env.Init()
	pushContext.InitContext(env, nil, nil)
	env.SetPushContext(pushContext)

	return env
}

var (
	defaultJSONLabelsOut = &fileaccesslog.FileAccessLog{
		Path: model.DevStdout,
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_JsonFormat{
					JsonFormat: model.EnvoyJSONLogFormatIstio,
				},
				JsonFormatOptions: &core.JsonFormatOptions{SortProperties: false},
			},
		},
	}

	defaultOut = &fileaccesslog.FileAccessLog{
		Path: model.DevStdout,
		AccessLogFormat: &fileaccesslog.FileAccessLog_LogFormat{
			LogFormat: &core.SubstitutionFormatString{
				Format: &core.SubstitutionFormatString_TextFormatSource{
					TextFormatSource: &core.DataSource{
						Specifier: &core.DataSource_InlineString{
							InlineString: model.EnvoyTextLogFormat,
						},
					},
				},
			},
		},
	}
)

func TestSetTCPAccessLog(t *testing.T) {
	b := newAccessLogBuilder()

	env := newTestEnvironment()

	cases := []struct {
		name     string
		push     *model.PushContext
		proxy    *model.Proxy
		tcp      *tcp.TcpProxy
		class    networking.ListenerClass
		expected *tcp.TcpProxy
	}{
		{
			name: "telemetry",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "test"}},
				IstioVersion:    &model.IstioVersion{Major: 1, Minor: 23},
			},
			tcp:   &tcp.TcpProxy{},
			class: networking.ListenerClassSidecarInbound,
			expected: &tcp.TcpProxy{
				AccessLog: []*accesslog.AccessLog{
					{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
					},
				},
			},
		},
		{
			name: "log-selector-unmatched-telemetry",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test-with-server-accesslog-filter"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "test-with-server-accesslog-filter"}},
				IstioVersion:    &model.IstioVersion{Major: 1, Minor: 23},
			},
			tcp:   &tcp.TcpProxy{},
			class: networking.ListenerClassSidecarOutbound,
			expected: &tcp.TcpProxy{
				AccessLog: []*accesslog.AccessLog{
					{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultOut)},
					},
				},
			},
		},
		{
			name: "without-telemetry",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "without-telemetry"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "without-telemetry"}},
				IstioVersion:    &model.IstioVersion{Major: 1, Minor: 23},
			},
			tcp:   &tcp.TcpProxy{},
			class: networking.ListenerClassSidecarInbound,
			expected: &tcp.TcpProxy{
				AccessLog: []*accesslog.AccessLog{
					{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultOut)},
					},
				},
			},
		},
		{
			name: "disable-accesslog",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test-disable-accesslog"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "test-disable-accesslog"}},
				IstioVersion:    &model.IstioVersion{Major: 1, Minor: 23},
			},
			tcp:      &tcp.TcpProxy{},
			class:    networking.ListenerClassSidecarInbound,
			expected: &tcp.TcpProxy{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b.setTCPAccessLog(tc.push, tc.proxy, tc.tcp, tc.class, nil)
			assert.Equal(t, tc.expected, tc.tcp)
		})
	}
}

func TestSetHttpAccessLog(t *testing.T) {
	b := newAccessLogBuilder()

	env := newTestEnvironment()

	cases := []struct {
		name     string
		push     *model.PushContext
		proxy    *model.Proxy
		hcm      *hcm.HttpConnectionManager
		class    networking.ListenerClass
		expected *hcm.HttpConnectionManager
	}{
		{
			name: "telemetry",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "test"}},
				IstioVersion:    &model.IstioVersion{Major: 1, Minor: 23},
			},
			hcm:   &hcm.HttpConnectionManager{},
			class: networking.ListenerClassSidecarInbound,
			expected: &hcm.HttpConnectionManager{
				AccessLog: []*accesslog.AccessLog{
					{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
					},
				},
			},
		},
		{
			name: "log-selector-unmatched-telemetry",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test-with-server-accesslog-filter"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "test-with-server-accesslog-filter"}},
				IstioVersion:    &model.IstioVersion{Major: 1, Minor: 23},
			},
			hcm:   &hcm.HttpConnectionManager{},
			class: networking.ListenerClassSidecarOutbound,
			expected: &hcm.HttpConnectionManager{
				AccessLog: []*accesslog.AccessLog{
					{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultOut)},
					},
				},
			},
		},
		{
			name: "without-telemetry",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "without-telemetry"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "without-telemetry"}},
				IstioVersion:    &model.IstioVersion{Major: 1, Minor: 23},
			},
			hcm:   &hcm.HttpConnectionManager{},
			class: networking.ListenerClassSidecarInbound,
			expected: &hcm.HttpConnectionManager{
				AccessLog: []*accesslog.AccessLog{
					{
						Name:       wellknown.FileAccessLog,
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultOut)},
					},
				},
			},
		},
		{
			name: "disable-accesslog",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test-disable-accesslog"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "test-disable-accesslog"}},
				IstioVersion:    &model.IstioVersion{Major: 1, Minor: 23},
			},
			hcm:      &hcm.HttpConnectionManager{},
			class:    networking.ListenerClassSidecarInbound,
			expected: &hcm.HttpConnectionManager{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b.setHTTPAccessLog(tc.push, tc.proxy, tc.hcm, tc.class, nil)
			assert.Equal(t, tc.expected, tc.hcm)
		})
	}
}

func TestSetListenerAccessLog(t *testing.T) {
	b := newAccessLogBuilder()

	env := newTestEnvironment()

	cases := []struct {
		name     string
		push     *model.PushContext
		proxy    *model.Proxy
		listener *listener.Listener
		class    networking.ListenerClass
		expected *listener.Listener
	}{
		{
			name: "telemetry",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "test"}},
			},
			listener: &listener.Listener{},
			class:    networking.ListenerClassSidecarInbound,
			expected: &listener.Listener{
				AccessLog: []*accesslog.AccessLog{
					{
						Name: wellknown.FileAccessLog,
						Filter: &accesslog.AccessLogFilter{
							FilterSpecifier: &accesslog.AccessLogFilter_ResponseFlagFilter{
								ResponseFlagFilter: &accesslog.ResponseFlagFilter{Flags: []string{"NR"}},
							},
						},
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultJSONLabelsOut)},
					},
				},
			},
		},
		{
			name: "log-selector-unmatched-telemetry",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test-with-server-accesslog-filter"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "test-with-server-accesslog-filter"}},
			},
			listener: &listener.Listener{},
			class:    networking.ListenerClassSidecarOutbound,
			expected: &listener.Listener{
				AccessLog: []*accesslog.AccessLog{
					{
						Name: wellknown.FileAccessLog,
						Filter: &accesslog.AccessLogFilter{
							FilterSpecifier: &accesslog.AccessLogFilter_ResponseFlagFilter{
								ResponseFlagFilter: &accesslog.ResponseFlagFilter{Flags: []string{"NR"}},
							},
						},
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultOut)},
					},
				},
			},
		},
		{
			name: "without-telemetry",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "without-telemetry"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "without-telemetry"}},
			},
			listener: &listener.Listener{},
			class:    networking.ListenerClassSidecarInbound,
			expected: &listener.Listener{
				AccessLog: []*accesslog.AccessLog{
					{
						Name: wellknown.FileAccessLog,
						Filter: &accesslog.AccessLogFilter{
							FilterSpecifier: &accesslog.AccessLogFilter_ResponseFlagFilter{
								ResponseFlagFilter: &accesslog.ResponseFlagFilter{Flags: []string{"NR"}},
							},
						},
						ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(defaultOut)},
					},
				},
			},
		},
		{
			name: "disable-accesslog",
			push: env.PushContext(),
			proxy: &model.Proxy{
				ConfigNamespace: "default",
				Labels:          map[string]string{"app": "test-disable-accesslog"},
				Metadata:        &model.NodeMetadata{Labels: map[string]string{"app": "test-disable-accesslog"}},
			},
			listener: &listener.Listener{},
			class:    networking.ListenerClassSidecarInbound,
			expected: &listener.Listener{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b.setListenerAccessLog(tc.push, tc.proxy, tc.listener, tc.class)
			assert.Equal(t, tc.expected, tc.listener)
		})
	}
}
