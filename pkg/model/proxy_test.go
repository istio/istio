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

package model_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/model"
	"istio.io/istio/pkg/test/util/assert"
)

func TestNodeMetadata(t *testing.T) {
	tests := []struct {
		name  string
		in    model.BootstrapNodeMetadata
		out   string
		inOut model.BootstrapNodeMetadata
	}{
		{
			"empty",
			model.BootstrapNodeMetadata{},
			"{}",
			model.BootstrapNodeMetadata{NodeMetadata: model.NodeMetadata{Raw: map[string]any{}}},
		},
		{
			"csvlists",
			model.BootstrapNodeMetadata{NodeMetadata: model.NodeMetadata{InstanceIPs: []string{"abc", "1.2.3.4"}}},
			`{"INSTANCE_IPS":"abc,1.2.3.4"}`,
			model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					InstanceIPs: []string{"abc", "1.2.3.4"},
					Raw: map[string]any{
						"INSTANCE_IPS": "abc,1.2.3.4",
					},
				},
			},
		},
		{
			"labels",
			model.BootstrapNodeMetadata{NodeMetadata: model.NodeMetadata{Labels: map[string]string{"foo": "bar"}}},
			`{"LABELS":{"foo":"bar"}}`,
			model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					Labels: map[string]string{"foo": "bar"},
					Raw: map[string]any{
						"LABELS": map[string]any{
							"foo": "bar",
						},
					},
				},
			},
		},
		{
			"proxy config",
			model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					ProxyConfig: (*model.NodeMetaProxyConfig)(&meshconfig.ProxyConfig{
						ConfigPath:             "foo",
						DrainDuration:          durationpb.New(time.Second * 5),
						ControlPlaneAuthPolicy: meshconfig.AuthenticationPolicy_MUTUAL_TLS,
						EnvoyAccessLogService: &meshconfig.RemoteService{
							Address: "address",
							TlsSettings: &v1alpha3.ClientTLSSettings{
								SubjectAltNames: []string{"san"},
							},
						},
					}),
				},
			},
			// nolint: lll
			`{"PROXY_CONFIG":{"configPath":"foo","drainDuration":"5s","controlPlaneAuthPolicy":"MUTUAL_TLS","envoyAccessLogService":{"address":"address","tlsSettings":{"subjectAltNames":["san"]}}}}`,
			model.BootstrapNodeMetadata{
				NodeMetadata: model.NodeMetadata{
					ProxyConfig: (*model.NodeMetaProxyConfig)(&meshconfig.ProxyConfig{
						ConfigPath:             "foo",
						DrainDuration:          durationpb.New(time.Second * 5),
						ControlPlaneAuthPolicy: meshconfig.AuthenticationPolicy_MUTUAL_TLS,
						EnvoyAccessLogService: &meshconfig.RemoteService{
							Address: "address",
							TlsSettings: &v1alpha3.ClientTLSSettings{
								SubjectAltNames: []string{"san"},
							},
						},
					}),
					Raw: map[string]any{
						"PROXY_CONFIG": map[string]any{
							"drainDuration":          "5s",
							"configPath":             "foo",
							"controlPlaneAuthPolicy": "MUTUAL_TLS",
							"envoyAccessLogService": map[string]any{
								"address": "address",
								"tlsSettings": map[string]any{
									"subjectAltNames": []any{"san"},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}
			if string(j) != tt.out {
				t.Errorf("Got json '%s', expected '%s'", string(j), tt.out)
			}
			var meta model.BootstrapNodeMetadata
			if err := json.Unmarshal(j, &meta); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}

			assert.Equal(t, (*meshconfig.ProxyConfig)(meta.NodeMetadata.ProxyConfig), (*meshconfig.ProxyConfig)(tt.inOut.NodeMetadata.ProxyConfig))
			// cmp cannot handle the type-alias in the metadata, so check them separately.
			meta.NodeMetadata.ProxyConfig = nil
			tt.inOut.NodeMetadata.ProxyConfig = nil
			assert.Equal(t, meta, tt.inOut)
		})
	}
}

func TestStringList(t *testing.T) {
	cases := []struct {
		in          string
		expect      model.StringList
		noRoundTrip bool
	}{
		{in: `"a,b,c"`, expect: []string{"a", "b", "c"}},
		{in: `"\"a,b,c"`, expect: []string{`"a`, "b", "c"}},
		{in: `"a"`, expect: []string{"a"}},
		{in: `""`, expect: []string{}},
		{in: `"123,@#$#,abcdef"`, expect: []string{"123", "@#$#", "abcdef"}},
	}
	for _, tt := range cases {
		t.Run(tt.in, func(t *testing.T) {
			var out model.StringList
			if err := json.Unmarshal([]byte(tt.in), &out); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(out, tt.expect) {
				t.Fatalf("Expected %v, got %v", tt.expect, out)
			}
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			if tt.noRoundTrip {
				return
			}
			if !reflect.DeepEqual(string(b), tt.in) {
				t.Fatalf("Expected %v, got %v", tt.in, string(b))
			}
		})
	}
	// Invalid case
	var out model.StringList
	assert.Error(t, json.Unmarshal([]byte("1"), &out))
}

func TestPodPortList(t *testing.T) {
	cases := []struct {
		name            string
		in              string
		expect          model.PodPortList
		expUnmarshalErr string
	}{
		{"no port", `"[]"`, model.PodPortList{}, ""},
		{"one port", `"[{\"name\":\"foo\",\"containerPort\":9080,\"protocol\":\"TCP\"}]"`, model.PodPortList{{"foo", 9080, "TCP"}}, ""},
		{
			"two ports",
			`"[{\"name\":\"foo\",\"containerPort\":9080,\"protocol\":\"TCP\"},{\"containerPort\":8888,\"protocol\":\"TCP\"}]"`,
			model.PodPortList{{"foo", 9080, "TCP"}, {ContainerPort: 8888, Protocol: "TCP"}},
			"",
		},
		{"invalid syntax", `[]`, nil, "invalid syntax"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var out model.PodPortList
			if err := json.Unmarshal([]byte(tt.in), &out); err != nil {
				if tt.expUnmarshalErr == "" {
					t.Fatal(err)
				}
				if out != nil {
					t.Fatalf("%s: Expected null unmarshal output but obtained a non-null one.", tt.name)
				}
				if err.Error() != tt.expUnmarshalErr {
					t.Fatalf("%s: Expected error: %s but got error: %s.", tt.name, tt.expUnmarshalErr, err.Error())
				}
				return
			}
			if !reflect.DeepEqual(out, tt.expect) {
				t.Fatalf("%s: Expected %v, got %v", tt.name, tt.expect, out)
			}
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(string(b), tt.in) {
				t.Fatalf("%s: Expected %v, got %v", tt.name, tt.in, string(b))
			}
		})
	}
}

func TestStringBool(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		expect string
	}{
		{"1", `"1"`, `"true"`},
		{"0", `"0"`, `"false"`},
		{"false", `"false"`, `"false"`},
		{"true", `"true"`, `"true"`},
		{"invalid input", `"foo"`, ``},
		{"no quotes", `true`, ``},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var out model.StringBool
			if err := json.Unmarshal([]byte(tt.in), &out); err != nil {
				if tt.expect == "" {
					return
				}
				t.Fatal(err)
			}
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(string(b), tt.expect) {
				t.Fatalf("Expected %v, got %v", tt.expect, string(b))
			}
		})
	}
}

const (
	k8sSeparator = "."
)

func TestSanitizeLocalityLabel(t *testing.T) {
	cases := []struct {
		name     string
		label    string
		expected string
	}{
		{
			name:     "with label",
			label:    "region/zone/subzone-1",
			expected: "region/zone/subzone-1",
		},
		{
			name:     "label with k8s label separator",
			label:    "region" + k8sSeparator + "zone" + k8sSeparator + "subzone-2",
			expected: "region/zone/subzone-2",
		},
		{
			name:     "label with both k8s label separators and slashes",
			label:    "region/zone/subzone.2",
			expected: "region/zone/subzone.2",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := model.SanitizeLocalityLabel(testCase.label)
			if got != testCase.expected {
				t.Errorf("expected locality %s, but got %s", testCase.expected, got)
			}
		})
	}
}
