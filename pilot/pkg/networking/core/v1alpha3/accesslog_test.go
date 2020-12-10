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
	httppb "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"
	"github.com/envoyproxy/go-control-plane/pkg/conversion"
	"github.com/envoyproxy/go-control-plane/pkg/wellknown"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pkg/util/protomarshal"
)

func TestListenerAccessLog(t *testing.T) {
	version18 := &model.IstioVersion{Major: 1, Minor: 8}
	version19 := &model.IstioVersion{Major: 1, Minor: 9}
	defaultFormatJSON, _ := protomarshal.ToJSON(EnvoyJSONLogFormat)
	ge19FormatJSON, _ := protomarshal.ToJSON(EnvoyJSONLogFormatIstio19)

	for _, tc := range []struct {
		name         string
		encoding     meshconfig.MeshConfig_AccessLogEncoding
		proxyVersion *model.IstioVersion
		format       string
		wantFormat   string
	}{
		{
			name:         "valid json object",
			encoding:     meshconfig.MeshConfig_JSON,
			proxyVersion: version18,
			format:       `{"foo": "bar"}`,
			wantFormat:   `{"foo":"bar"}`,
		},
		{
			name:         "valid nested json object",
			encoding:     meshconfig.MeshConfig_JSON,
			proxyVersion: version18,
			format:       `{"foo": {"bar": "ha"}}`,
			wantFormat:   `{"foo":{"bar":"ha"}}`,
		},
		{
			name:         "invalid json object",
			encoding:     meshconfig.MeshConfig_JSON,
			proxyVersion: version18,
			format:       `foo`,
			wantFormat:   defaultFormatJSON,
		},
		{
			name:         "incorrect json type",
			encoding:     meshconfig.MeshConfig_JSON,
			proxyVersion: version18,
			format:       `[]`,
			wantFormat:   defaultFormatJSON,
		},
		{
			name:         "incorrect json type",
			encoding:     meshconfig.MeshConfig_JSON,
			proxyVersion: version18,
			format:       `"{}"`,
			wantFormat:   defaultFormatJSON,
		},
		{
			name:         "default json format",
			encoding:     meshconfig.MeshConfig_JSON,
			proxyVersion: version18,
			wantFormat:   defaultFormatJSON,
		},
		{
			name:         "default json format proxy 1.9",
			encoding:     meshconfig.MeshConfig_JSON,
			proxyVersion: version19,
			wantFormat:   ge19FormatJSON,
		},
		{
			name:         "default text format",
			encoding:     meshconfig.MeshConfig_TEXT,
			proxyVersion: version18,
			wantFormat:   EnvoyTextLogFormat,
		},
		{
			name:         "default text format proxy 1.9",
			encoding:     meshconfig.MeshConfig_TEXT,
			proxyVersion: version19,
			wantFormat:   EnvoyTextLogFormatIstio19,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Update MeshConfig
			env := buildListenerEnv(nil)
			env.Mesh().AccessLogFile = "foo"
			env.Mesh().AccessLogEncoding = tc.encoding
			env.Mesh().AccessLogFormat = tc.format

			// Trigger MeshConfig change and validate that access log is recomputed.
			accessLogBuilder.reset()

			// Validate that access log filter uses the new format.
			listeners := buildAllListeners(&fakePlugin{}, env, tc.proxyVersion)
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
		textFormatString := cfg.GetFields()["log_format"].GetStructValue().GetFields()["text_format"].GetStringValue()
		if textFormatString != wantFormat {
			t.Errorf("\nwant: %s\n got: %s", wantFormat, textFormatString)
		}
	}
}
