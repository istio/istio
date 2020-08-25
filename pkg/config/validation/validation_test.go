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

package validation

import (
	"strings"
	"testing"
	"time"

	wellknown "github.com/envoyproxy/go-control-plane/pkg/wellknown"
	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/hashicorp/go-multierror"

	meshconfig "istio.io/api/mesh/v1alpha1"
	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
	networking "istio.io/api/networking/v1alpha3"
	security_beta "istio.io/api/security/v1beta1"
	api "istio.io/api/type/v1beta1"

	"istio.io/istio/pkg/config/constants"
)

const (
	// Config name for testing
	someName = "foo"
	// Config namespace for testing.
	someNamespace = "bar"
)

func TestValidateFQDN(t *testing.T) {
	tests := []struct {
		fqdn  string
		valid bool
		name  string
	}{
		{
			fqdn:  strings.Repeat("x", 256),
			valid: false,
			name:  "long FQDN",
		},
		{
			fqdn:  "",
			valid: false,
			name:  "empty FQDN",
		},
		{
			fqdn:  "istio.io",
			valid: true,
			name:  "standard FQDN",
		},
		{
			fqdn:  "istio.io.",
			valid: true,
			name:  "unambiguous FQDN",
		},
		{
			fqdn:  "istio-pilot.istio-system.svc.cluster.local",
			valid: true,
			name:  "standard kubernetes FQDN",
		},
		{
			fqdn:  "istio-pilot.istio-system.svc.cluster.local.",
			valid: true,
			name:  "unambiguous kubernetes FQDN",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFQDN(tt.fqdn)
			valid := err == nil
			if valid != tt.valid {
				t.Errorf("Expected valid=%v, got valid=%v for %v", tt.valid, valid, tt.fqdn)
			}
		})

	}
}

func TestValidateWildcardDomain(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  string
	}{
		{"empty", "", "empty"},
		{"too long", strings.Repeat("x", 256), "too long"},
		{"happy", strings.Repeat("x", 63), ""},
		{"wildcard", "*", ""},
		{"wildcard multi-segment", "*.bar.com", ""},
		{"wildcard single segment", "*foo", ""},
		{"wildcard prefix", "*foo.bar.com", ""},
		{"wildcard prefix dash", "*-foo.bar.com", ""},
		{"bad wildcard", "foo.*.com", "invalid"},
		{"bad wildcard", "foo*.bar.com", "invalid"},
		{"IP address", "1.1.1.1", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWildcardDomain(tt.in)
			if err == nil && tt.out != "" {
				t.Fatalf("ValidateWildcardDomain(%v) = nil, wanted %q", tt.in, tt.out)
			} else if err != nil && tt.out == "" {
				t.Fatalf("ValidateWildcardDomain(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.out) {
				t.Fatalf("ValidateWildcardDomain(%v) = %v, wanted %q", tt.in, err, tt.out)
			}
		})
	}
}

func TestValidatePort(t *testing.T) {
	ports := map[int]bool{
		0:     false,
		65536: false,
		-1:    false,
		100:   true,
		1000:  true,
		65535: true,
	}
	for port, valid := range ports {
		if got := ValidatePort(port); (got == nil) != valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %d", got == nil, valid, got, port)
		}
	}
}

func TestValidateProxyAddress(t *testing.T) {
	addresses := map[string]bool{
		"istio-pilot:80":        true,
		"istio-pilot":           false,
		"isti..:80":             false,
		"10.0.0.100:9090":       true,
		"10.0.0.100":            false,
		"istio-pilot:port":      false,
		"istio-pilot:100000":    false,
		"[2001:db8::100]:80":    true,
		"[2001:db8::10::20]:80": false,
		"[2001:db8::100]":       false,
		"[2001:db8::100]:port":  false,
		"2001:db8::100:80":      false,
	}
	for addr, valid := range addresses {
		if got := ValidateProxyAddress(addr); (got == nil) != valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %s", got == nil, valid, got, addr)
		}
	}
}

func TestValidateDuration(t *testing.T) {
	type durationCheck struct {
		duration *types.Duration
		isValid  bool
	}

	checks := []durationCheck{
		{
			duration: &types.Duration{Seconds: 1},
			isValid:  true,
		},
		{
			duration: &types.Duration{Seconds: 1, Nanos: -1},
			isValid:  false,
		},
		{
			duration: &types.Duration{Seconds: -11, Nanos: -1},
			isValid:  false,
		},
		{
			duration: &types.Duration{Nanos: 1},
			isValid:  false,
		},
		{
			duration: &types.Duration{Seconds: 1, Nanos: 1},
			isValid:  false,
		},
	}

	for _, check := range checks {
		if got := ValidateDuration(check.duration); (got == nil) != check.isValid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, check.isValid, got, check.duration)
		}
	}
}

func TestValidateParentAndDrain(t *testing.T) {
	type ParentDrainTime struct {
		Parent types.Duration
		Drain  types.Duration
		Valid  bool
	}

	combinations := []ParentDrainTime{
		{
			Parent: types.Duration{Seconds: 2},
			Drain:  types.Duration{Seconds: 1},
			Valid:  true,
		},
		{
			Parent: types.Duration{Seconds: 1},
			Drain:  types.Duration{Seconds: 1},
			Valid:  false,
		},
		{
			Parent: types.Duration{Seconds: 1},
			Drain:  types.Duration{Seconds: 2},
			Valid:  false,
		},
		{
			Parent: types.Duration{Seconds: 2},
			Drain:  types.Duration{Seconds: 1, Nanos: 1000000},
			Valid:  false,
		},
		{
			Parent: types.Duration{Seconds: 2, Nanos: 1000000},
			Drain:  types.Duration{Seconds: 1},
			Valid:  false,
		},
		{
			Parent: types.Duration{Seconds: -2},
			Drain:  types.Duration{Seconds: 1},
			Valid:  false,
		},
		{
			Parent: types.Duration{Seconds: 2},
			Drain:  types.Duration{Seconds: -1},
			Valid:  false,
		},
		{
			Parent: types.Duration{Seconds: 1 + int64(time.Hour/time.Second)},
			Drain:  types.Duration{Seconds: 10},
			Valid:  false,
		},
		{
			Parent: types.Duration{Seconds: 10},
			Drain:  types.Duration{Seconds: 1 + int64(time.Hour/time.Second)},
			Valid:  false,
		},
	}
	for _, combo := range combinations {
		if got := ValidateParentAndDrain(&combo.Drain, &combo.Parent); (got == nil) != combo.Valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for Parent:%v Drain:%v",
				got == nil, combo.Valid, got, combo.Parent, combo.Drain)
		}
	}
}

func TestValidateConnectTimeout(t *testing.T) {
	type durationCheck struct {
		duration *types.Duration
		isValid  bool
	}

	checks := []durationCheck{
		{
			duration: &types.Duration{Seconds: 1},
			isValid:  true,
		},
		{
			duration: &types.Duration{Seconds: 31},
			isValid:  false,
		},
		{
			duration: &types.Duration{Nanos: 99999},
			isValid:  false,
		},
	}

	for _, check := range checks {
		if got := ValidateConnectTimeout(check.duration); (got == nil) != check.isValid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, check.isValid, got, check.duration)
		}
	}
}

func TestValidateProtocolDetectionTimeout(t *testing.T) {
	type durationCheck struct {
		duration *types.Duration
		isValid  bool
	}

	checks := []durationCheck{
		{
			duration: &types.Duration{Seconds: 1},
			isValid:  true,
		},
		{
			duration: &types.Duration{Nanos: 99999},
			isValid:  false,
		},
		{
			duration: &types.Duration{Nanos: 0},
			isValid:  true,
		},
	}

	for _, check := range checks {
		if got := ValidateProtocolDetectionTimeout(check.duration); (got == nil) != check.isValid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, check.isValid, got, check.duration)
		}
	}
}

func TestValidateMeshConfig(t *testing.T) {
	if ValidateMeshConfig(&meshconfig.MeshConfig{}) == nil {
		t.Error("expected an error on an empty mesh config")
	}

	invalid := meshconfig.MeshConfig{
		MixerCheckServer:  "10.0.0.100",
		MixerReportServer: "10.0.0.100",
		ProxyListenPort:   0,
		ConnectTimeout:    types.DurationProto(-1 * time.Second),
		DefaultConfig:     &meshconfig.ProxyConfig{},
	}

	err := ValidateMeshConfig(&invalid)
	if err == nil {
		t.Errorf("expected an error on invalid proxy mesh config: %v", invalid)
	} else {
		switch err := err.(type) {
		case *multierror.Error:
			// each field must cause an error in the field
			if len(err.Errors) < 6 {
				t.Errorf("expected an error for each field %v", err)
			}
		default:
			t.Errorf("expected a multi error as output")
		}
	}
}

func TestValidateProxyConfig(t *testing.T) {
	valid := &meshconfig.ProxyConfig{
		ConfigPath:             "/etc/istio/proxy",
		BinaryPath:             "/usr/local/bin/envoy",
		DiscoveryAddress:       "istio-pilot.istio-system:15010",
		ProxyAdminPort:         15000,
		DrainDuration:          types.DurationProto(45 * time.Second),
		ParentShutdownDuration: types.DurationProto(60 * time.Second),
		ServiceCluster:         "istio-proxy",
		StatsdUdpAddress:       "istio-statsd-prom-bridge.istio-system:9125",
		EnvoyMetricsService:    &meshconfig.RemoteService{Address: "metrics-service.istio-system:15000"},
		EnvoyAccessLogService:  &meshconfig.RemoteService{Address: "accesslog-service.istio-system:15000"},
		ControlPlaneAuthPolicy: meshconfig.AuthenticationPolicy_MUTUAL_TLS,
		Tracing:                nil,
	}

	modify := func(config *meshconfig.ProxyConfig, fieldSetter func(*meshconfig.ProxyConfig)) *meshconfig.ProxyConfig {
		clone := proto.Clone(config).(*meshconfig.ProxyConfig)
		fieldSetter(clone)
		return clone
	}

	cases := []struct {
		name    string
		in      *meshconfig.ProxyConfig
		isValid bool
	}{
		{
			name:    "empty proxy config",
			in:      &meshconfig.ProxyConfig{},
			isValid: false,
		},
		{
			name:    "valid proxy config",
			in:      valid,
			isValid: true,
		},
		{
			name:    "config path invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ConfigPath = "" }),
			isValid: false,
		},
		{
			name:    "binary path invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.BinaryPath = "" }),
			isValid: false,
		},
		{
			name:    "discovery address invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.DiscoveryAddress = "10.0.0.100" }),
			isValid: false,
		},
		{
			name:    "proxy admin port invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ProxyAdminPort = 0 }),
			isValid: false,
		},
		{
			name:    "proxy admin port invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ProxyAdminPort = 0 }),
			isValid: false,
		},
		{
			name:    "drain duration invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.DrainDuration = types.DurationProto(-1 * time.Second) }),
			isValid: false,
		},
		{
			name:    "parent shutdown duration invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ParentShutdownDuration = types.DurationProto(-1 * time.Second) }),
			isValid: false,
		},
		{
			name:    "service cluster invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ServiceCluster = "" }),
			isValid: false,
		},
		{
			name:    "statsd udp address invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.StatsdUdpAddress = "10.0.0.100" }),
			isValid: false,
		},
		{
			name: "envoy metrics service address invalid",
			in: modify(valid, func(c *meshconfig.ProxyConfig) {
				c.EnvoyMetricsService = &meshconfig.RemoteService{Address: "metrics-service.istio-system"}
			}),
			isValid: false,
		},
		{
			name: "envoy access log service address invalid",
			in: modify(valid, func(c *meshconfig.ProxyConfig) {
				c.EnvoyAccessLogService = &meshconfig.RemoteService{Address: "accesslog-service.istio-system"}
			}),
			isValid: false,
		},
		{
			name:    "control plane auth policy invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ControlPlaneAuthPolicy = -1 }),
			isValid: false,
		},
		{
			name: "zipkin address is valid",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Zipkin_{
							Zipkin: &meshconfig.Tracing_Zipkin{
								Address: "zipkin.istio-system:9411",
							},
						},
					}
				},
			),
			isValid: true,
		},
		{
			name: "zipkin config invalid",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Zipkin_{
							Zipkin: &meshconfig.Tracing_Zipkin{
								Address: "10.0.0.100",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep config is valid",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "collector.lightstep:8080",
								AccessToken: "abcdefg1234567",
							},
						},
					}
				},
			),
			isValid: true,
		},
		{
			name: "lightstep address invalid",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "10.0.0.100",
								AccessToken: "abcdefg1234567",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep address empty but lightstep access token is not",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "",
								AccessToken: "abcdefg1234567",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep address is valid but access token is empty",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "collector.lightstep:8080",
								AccessToken: "",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep access token empty but lightstep address is not",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "10.0.0.100",
								AccessToken: "",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep address and lightstep token both empty",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "",
								AccessToken: "",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "datadog without address",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Datadog_{
							Datadog: &meshconfig.Tracing_Datadog{},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "datadog with correct address",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Datadog_{
							Datadog: &meshconfig.Tracing_Datadog{
								Address: "datadog-agent:8126",
							},
						},
					}
				},
			),
			isValid: true,
		},
		{
			name: "datadog with invalid address",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Datadog_{
							Datadog: &meshconfig.Tracing_Datadog{
								Address: "address-missing-port-number",
							},
						},
					}
				},
			),
			isValid: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateProxyConfig(c.in); (got == nil) != c.isValid {
				if c.isValid {
					t.Errorf("got error %v, wanted none", got)
				} else {
					t.Error("got no error, wanted one")
				}
			}
		})
	}

	invalid := meshconfig.ProxyConfig{
		ConfigPath:             "",
		BinaryPath:             "",
		DiscoveryAddress:       "10.0.0.100",
		ProxyAdminPort:         0,
		DrainDuration:          types.DurationProto(-1 * time.Second),
		ParentShutdownDuration: types.DurationProto(-1 * time.Second),
		ServiceCluster:         "",
		StatsdUdpAddress:       "10.0.0.100",
		EnvoyMetricsService:    &meshconfig.RemoteService{Address: "metrics-service"},
		EnvoyAccessLogService:  &meshconfig.RemoteService{Address: "accesslog-service"},
		ControlPlaneAuthPolicy: -1,
		Tracing: &meshconfig.Tracing{
			Tracer: &meshconfig.Tracing_Zipkin_{
				Zipkin: &meshconfig.Tracing_Zipkin{
					Address: "10.0.0.100",
				},
			},
		},
	}

	err := ValidateProxyConfig(&invalid)
	if err == nil {
		t.Errorf("expected an error on invalid proxy mesh config: %v", invalid)
	} else {
		switch err := err.(type) {
		case *multierror.Error:
			// each field must cause an error in the field
			if len(err.Errors) != 12 {
				t.Errorf("expected an error for each field %v", err)
			}
		default:
			t.Errorf("expected a multi error as output")
		}
	}
}

var (
	validService    = &mccpb.IstioService{Service: "*cnn.com"}
	validAttributes = &mpb.Attributes{
		Attributes: map[string]*mpb.Attributes_AttributeValue{
			"api.service": {Value: &mpb.Attributes_AttributeValue_StringValue{"my-service"}},
		},
	}
	invalidAttributes = &mpb.Attributes{
		Attributes: map[string]*mpb.Attributes_AttributeValue{
			"api.service": {Value: &mpb.Attributes_AttributeValue_StringValue{""}},
		},
	}
)

func TestValidateMixerAttributes(t *testing.T) {
	cases := []struct {
		name  string
		in    *mpb.Attributes_AttributeValue
		valid bool
	}{
		{"happy string",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_StringValue{"my-service"}},
			true},
		{"invalid string",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_StringValue{""}},
			false},
		{"happy duration",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_DurationValue{&types.Duration{Seconds: 1}}},
			true},
		{"invalid duration",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_DurationValue{&types.Duration{Nanos: -1e9}}},
			false},
		{"happy bytes",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_BytesValue{[]byte{1, 2, 3}}},
			true},
		{"invalid bytes",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_BytesValue{[]byte{}}},
			false},
		{"happy timestamp",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_TimestampValue{&types.Timestamp{}}},
			true},
		{"invalid timestamp",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_TimestampValue{&types.Timestamp{Nanos: -1}}},
			false},
		{"nil timestamp",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_TimestampValue{nil}},
			false},
		{"happy stringmap",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_StringMapValue{
				&mpb.Attributes_StringMap{Entries: map[string]string{"foo": "bar"}}}},
			true},
		{"invalid stringmap",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_StringMapValue{
				&mpb.Attributes_StringMap{Entries: nil}}},
			false},
		{"nil stringmap",
			&mpb.Attributes_AttributeValue{Value: &mpb.Attributes_AttributeValue_StringMapValue{nil}},
			false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			attrs := &mpb.Attributes{
				Attributes: map[string]*mpb.Attributes_AttributeValue{"key": c.in},
			}
			if got := ValidateMixerAttributes(attrs); (got == nil) != c.valid {
				if c.valid {
					t.Fatal("got error, wanted none")
				} else {
					t.Fatal("got no error, wanted one")
				}
			}
		})
	}
}

func TestValidateHTTPAPISpec(t *testing.T) {
	var (
		validPattern = &mccpb.HTTPAPISpecPattern{
			Attributes: validAttributes,
			HttpMethod: "POST",
			Pattern: &mccpb.HTTPAPISpecPattern_UriTemplate{
				UriTemplate: "/pet/{id}",
			},
		}
		invalidPatternHTTPMethod = &mccpb.HTTPAPISpecPattern{
			Attributes: validAttributes,
			Pattern: &mccpb.HTTPAPISpecPattern_UriTemplate{
				UriTemplate: "/pet/{id}",
			},
		}
		invalidPatternURITemplate = &mccpb.HTTPAPISpecPattern{
			Attributes: validAttributes,
			HttpMethod: "POST",
			Pattern:    &mccpb.HTTPAPISpecPattern_UriTemplate{},
		}
		invalidPatternRegex = &mccpb.HTTPAPISpecPattern{
			Attributes: validAttributes,
			HttpMethod: "POST",
			Pattern:    &mccpb.HTTPAPISpecPattern_Regex{},
		}
		validAPIKey         = &mccpb.APIKey{Key: &mccpb.APIKey_Query{"api_key"}}
		invalidAPIKeyQuery  = &mccpb.APIKey{Key: &mccpb.APIKey_Query{}}
		invalidAPIKeyHeader = &mccpb.APIKey{Key: &mccpb.APIKey_Header{}}
		invalidAPIKeyCookie = &mccpb.APIKey{Key: &mccpb.APIKey_Cookie{}}
	)

	cases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{
			name: "missing pattern",
			in: &mccpb.HTTPAPISpec{
				Attributes: validAttributes,
				ApiKeys:    []*mccpb.APIKey{validAPIKey},
			},
		},
		{
			name: "invalid pattern (bad attributes)",
			in: &mccpb.HTTPAPISpec{
				Attributes: invalidAttributes,
				Patterns:   []*mccpb.HTTPAPISpecPattern{validPattern},
				ApiKeys:    []*mccpb.APIKey{validAPIKey},
			},
		},
		{
			name: "invalid pattern (bad http_method)",
			in: &mccpb.HTTPAPISpec{
				Attributes: validAttributes,
				Patterns:   []*mccpb.HTTPAPISpecPattern{invalidPatternHTTPMethod},
				ApiKeys:    []*mccpb.APIKey{validAPIKey},
			},
		},
		{
			name: "invalid pattern (missing uri_template)",
			in: &mccpb.HTTPAPISpec{
				Attributes: validAttributes,
				Patterns:   []*mccpb.HTTPAPISpecPattern{invalidPatternURITemplate},
				ApiKeys:    []*mccpb.APIKey{validAPIKey},
			},
		},
		{
			name: "invalid pattern (missing regex)",
			in: &mccpb.HTTPAPISpec{
				Attributes: validAttributes,
				Patterns:   []*mccpb.HTTPAPISpecPattern{invalidPatternRegex},
				ApiKeys:    []*mccpb.APIKey{validAPIKey},
			},
		},
		{
			name: "invalid api-key (missing query)",
			in: &mccpb.HTTPAPISpec{
				Attributes: validAttributes,
				Patterns:   []*mccpb.HTTPAPISpecPattern{validPattern},
				ApiKeys:    []*mccpb.APIKey{invalidAPIKeyQuery},
			},
		},
		{
			name: "invalid api-key (missing header)",
			in: &mccpb.HTTPAPISpec{
				Attributes: validAttributes,
				Patterns:   []*mccpb.HTTPAPISpecPattern{validPattern},
				ApiKeys:    []*mccpb.APIKey{invalidAPIKeyHeader},
			},
		},
		{
			name: "invalid api-key (missing cookie)",
			in: &mccpb.HTTPAPISpec{
				Attributes: validAttributes,
				Patterns:   []*mccpb.HTTPAPISpecPattern{validPattern},
				ApiKeys:    []*mccpb.APIKey{invalidAPIKeyCookie},
			},
		},
		{
			name: "valid",
			in: &mccpb.HTTPAPISpec{
				Attributes: validAttributes,
				Patterns:   []*mccpb.HTTPAPISpecPattern{validPattern},
				ApiKeys:    []*mccpb.APIKey{validAPIKey},
			},
			valid: true,
		},
		{
			name: "invalid attribute (nil)",
			in: &mccpb.HTTPAPISpec{
				Attributes: &mpb.Attributes{
					Attributes: map[string]*mpb.Attributes_AttributeValue{"": nil},
				},
			},
			valid: false,
		},
	}
	for _, c := range cases {
		if got := ValidateHTTPAPISpec(someName, someNamespace, c.in); (got == nil) != c.valid {
			t.Errorf("ValidateHTTPAPISpec(%v): got(%v) != want(%v): %v", c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateHTTPAPISpecBinding(t *testing.T) {
	var (
		validHTTPAPISpecRef   = &mccpb.HTTPAPISpecReference{Name: "foo", Namespace: "bar"}
		invalidHTTPAPISpecRef = &mccpb.HTTPAPISpecReference{Name: "foo", Namespace: "--bar"}
	)
	cases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{
			name: "no service",
			in: &mccpb.HTTPAPISpecBinding{
				Services: []*mccpb.IstioService{},
				ApiSpecs: []*mccpb.HTTPAPISpecReference{validHTTPAPISpecRef},
			},
		},
		{
			name: "no spec",
			in: &mccpb.HTTPAPISpecBinding{
				Services: []*mccpb.IstioService{validService},
				ApiSpecs: []*mccpb.HTTPAPISpecReference{},
			},
		},
		{
			name: "invalid spec",
			in: &mccpb.HTTPAPISpecBinding{
				Services: []*mccpb.IstioService{validService},
				ApiSpecs: []*mccpb.HTTPAPISpecReference{invalidHTTPAPISpecRef},
			},
		},
		{
			name: "valid",
			in: &mccpb.HTTPAPISpecBinding{
				Services: []*mccpb.IstioService{validService},
				ApiSpecs: []*mccpb.HTTPAPISpecReference{validHTTPAPISpecRef},
			},
			valid: true,
		},
	}
	for _, c := range cases {
		if got := ValidateHTTPAPISpecBinding(someName, someNamespace, c.in); (got == nil) != c.valid {
			t.Errorf("ValidateHTTPAPISpecBinding(%v): got(%v) != want(%v): %v", c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateQuotaSpec(t *testing.T) {
	var (
		validMatch = &mccpb.AttributeMatch{
			Clause: map[string]*mccpb.StringMatch{
				"api.operation": {
					MatchType: &mccpb.StringMatch_Exact{
						Exact: "getPet",
					},
				},
			},
		}
		invalidMatchExact = &mccpb.AttributeMatch{
			Clause: map[string]*mccpb.StringMatch{
				"api.operation": {
					MatchType: &mccpb.StringMatch_Exact{Exact: ""},
				},
			},
		}
		invalidMatchPrefix = &mccpb.AttributeMatch{
			Clause: map[string]*mccpb.StringMatch{
				"api.operation": {
					MatchType: &mccpb.StringMatch_Prefix{Prefix: ""},
				},
			},
		}
		invalidMatchRegex = &mccpb.AttributeMatch{
			Clause: map[string]*mccpb.StringMatch{
				"api.operation": {
					MatchType: &mccpb.StringMatch_Regex{Regex: ""},
				},
			},
		}
		invalidQuota = &mccpb.Quota{
			Quota:  "",
			Charge: 0,
		}
		validQuota = &mccpb.Quota{
			Quota:  "myQuota",
			Charge: 2,
		}
	)
	cases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{
			name: "no rules",
			in: &mccpb.QuotaSpec{
				Rules: []*mccpb.QuotaRule{{}},
			},
		},
		{
			name: "invalid match (exact)",
			in: &mccpb.QuotaSpec{
				Rules: []*mccpb.QuotaRule{{
					Match:  []*mccpb.AttributeMatch{invalidMatchExact},
					Quotas: []*mccpb.Quota{validQuota},
				}},
			},
		},
		{
			name: "invalid match (prefix)",
			in: &mccpb.QuotaSpec{
				Rules: []*mccpb.QuotaRule{{
					Match:  []*mccpb.AttributeMatch{invalidMatchPrefix},
					Quotas: []*mccpb.Quota{validQuota},
				}},
			},
		},
		{
			name: "invalid match (regex)",
			in: &mccpb.QuotaSpec{
				Rules: []*mccpb.QuotaRule{{
					Match:  []*mccpb.AttributeMatch{invalidMatchRegex},
					Quotas: []*mccpb.Quota{validQuota},
				}},
			},
		},
		{
			name: "no quota",
			in: &mccpb.QuotaSpec{
				Rules: []*mccpb.QuotaRule{{
					Match:  []*mccpb.AttributeMatch{validMatch},
					Quotas: []*mccpb.Quota{},
				}},
			},
		},
		{
			name: "invalid quota/charge",
			in: &mccpb.QuotaSpec{
				Rules: []*mccpb.QuotaRule{{
					Match:  []*mccpb.AttributeMatch{validMatch},
					Quotas: []*mccpb.Quota{invalidQuota},
				}},
			},
		},
		{
			name: "valid",
			in: &mccpb.QuotaSpec{
				Rules: []*mccpb.QuotaRule{{
					Match:  []*mccpb.AttributeMatch{validMatch},
					Quotas: []*mccpb.Quota{validQuota},
				}},
			},
			valid: true,
		},
		{
			name: "regression test - nil clause",
			in: &mccpb.QuotaSpec{
				Rules: []*mccpb.QuotaRule{{
					Match: []*mccpb.AttributeMatch{{
						Clause: map[string]*mccpb.StringMatch{
							"": nil,
						},
					}},
				}},
			},
			valid: false,
		},
	}
	for _, c := range cases {
		if got := ValidateQuotaSpec(someName, someNamespace, c.in); (got == nil) != c.valid {
			t.Errorf("ValidateQuotaSpec(%v): got(%v) != want(%v): %v", c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateQuotaSpecBinding(t *testing.T) {
	var (
		validQuotaSpecRef   = &mccpb.QuotaSpecBinding_QuotaSpecReference{Name: "foo", Namespace: "bar"}
		invalidQuotaSpecRef = &mccpb.QuotaSpecBinding_QuotaSpecReference{Name: "foo", Namespace: "--bar"}
	)
	cases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{
			name: "no service",
			in: &mccpb.QuotaSpecBinding{
				Services:   []*mccpb.IstioService{},
				QuotaSpecs: []*mccpb.QuotaSpecBinding_QuotaSpecReference{validQuotaSpecRef},
			},
		},
		{
			name: "no spec",
			in: &mccpb.QuotaSpecBinding{
				Services:   []*mccpb.IstioService{validService},
				QuotaSpecs: []*mccpb.QuotaSpecBinding_QuotaSpecReference{},
			},
		},
		{
			name: "invalid spec",
			in: &mccpb.QuotaSpecBinding{
				Services:   []*mccpb.IstioService{validService},
				QuotaSpecs: []*mccpb.QuotaSpecBinding_QuotaSpecReference{invalidQuotaSpecRef},
			},
		},
		{
			name: "valid",
			in: &mccpb.QuotaSpecBinding{
				Services:   []*mccpb.IstioService{validService},
				QuotaSpecs: []*mccpb.QuotaSpecBinding_QuotaSpecReference{validQuotaSpecRef},
			},
			valid: true,
		},
	}
	for _, c := range cases {
		if got := ValidateQuotaSpecBinding(someName, someNamespace, c.in); (got == nil) != c.valid {
			t.Errorf("ValidateQuotaSpecBinding(%v): got(%v) != want(%v): %v", c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateGateway(t *testing.T) {
	tests := []struct {
		name string
		in   proto.Message
		out  string
	}{
		{"empty", &networking.Gateway{}, "server"},
		{"invalid message", &networking.Server{}, "cannot cast"},
		{"happy domain",
			&networking.Gateway{
				Servers: []*networking.Server{{
					Hosts: []string{"foo.bar.com"},
					Port:  &networking.Port{Name: "name1", Number: 7, Protocol: "http"},
				}},
			},
			""},
		{"happy multiple servers",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"foo.bar.com"},
						Port:  &networking.Port{Name: "name1", Number: 7, Protocol: "http"},
					}},
			},
			""},
		{"invalid port",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"foo.bar.com"},
						Port:  &networking.Port{Name: "name1", Number: 66000, Protocol: "http"},
					}},
			},
			"port"},
		{"duplicate port names",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"foo.bar.com"},
						Port:  &networking.Port{Name: "foo", Number: 80, Protocol: "http"},
					},
					{
						Hosts: []string{"scooby.doo.com"},
						Port:  &networking.Port{Name: "foo", Number: 8080, Protocol: "http"},
					}},
			},
			"port names"},
		{"invalid domain",
			&networking.Gateway{
				Servers: []*networking.Server{
					{
						Hosts: []string{"foo.*.bar.com"},
						Port:  &networking.Port{Number: 7, Protocol: "http"},
					}},
			},
			"domain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGateway(someName, someNamespace, tt.in)
			if err == nil && tt.out != "" {
				t.Fatalf("ValidateGateway(%v) = nil, wanted %q", tt.in, tt.out)
			} else if err != nil && tt.out == "" {
				t.Fatalf("ValidateGateway(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.out) {
				t.Fatalf("ValidateGateway(%v) = %v, wanted %q", tt.in, err, tt.out)
			}
		})
	}
}

func TestValidateServer(t *testing.T) {
	tests := []struct {
		name string
		in   *networking.Server
		out  string
	}{
		{"empty", &networking.Server{}, "host"},
		{"empty", &networking.Server{}, "port"},
		{"happy",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			""},
		{"happy ip",
			&networking.Server{
				Hosts: []string{"1.1.1.1"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			""},
		{"happy ns/name",
			&networking.Server{
				Hosts: []string{"ns1/foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			""},
		{"happy */name",
			&networking.Server{
				Hosts: []string{"*/foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			""},
		{"happy ./name",
			&networking.Server{
				Hosts: []string{"./foo.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			""},
		{"invalid domain ns/name format",
			&networking.Server{
				Hosts: []string{"ns1/foo.*.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"domain"},
		{"invalid domain",
			&networking.Server{
				Hosts: []string{"foo.*.bar.com"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"domain"},
		{"invalid short name host",
			&networking.Server{
				Hosts: []string{"foo"},
				Port:  &networking.Port{Number: 7, Name: "http", Protocol: "http"},
			},
			"short names"},
		{"invalid port",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 66000, Name: "http", Protocol: "http"},
			},
			"port"},
		{"invalid tls options",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 1, Name: "http", Protocol: "http"},
				Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_SIMPLE},
			},
			"TLS"},
		{"no tls on HTTPS",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 10000, Name: "https", Protocol: "https"},
			},
			"must have TLS"},
		{"tls on HTTP",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 10000, Name: "http", Protocol: "http"},
				Tls:   &networking.ServerTLSSettings{Mode: networking.ServerTLSSettings_SIMPLE},
			},
			"cannot have TLS"},
		{"tls redirect on HTTP",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 10000, Name: "http", Protocol: "http"},
				Tls: &networking.ServerTLSSettings{
					HttpsRedirect: true,
				},
			},
			""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateServer(tt.in)
			if err == nil && tt.out != "" {
				t.Fatalf("validateServer(%v) = nil, wanted %q", tt.in, tt.out)
			} else if err != nil && tt.out == "" {
				t.Fatalf("validateServer(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.out) {
				t.Fatalf("validateServer(%v) = %v, wanted %q", tt.in, err, tt.out)
			}
		})
	}
}

func TestValidateServerPort(t *testing.T) {
	tests := []struct {
		name string
		in   *networking.Port
		out  string
	}{
		{"empty", &networking.Port{}, "invalid protocol"},
		{"empty", &networking.Port{}, "port name"},
		{"happy",
			&networking.Port{
				Protocol: "http",
				Number:   1,
				Name:     "Henry",
			},
			""},
		{"invalid protocol",
			&networking.Port{
				Protocol: "kafka",
				Number:   1,
				Name:     "Henry",
			},
			"invalid protocol"},
		{"invalid number",
			&networking.Port{
				Protocol: "http",
				Number:   uint32(1 << 30),
				Name:     "http",
			},
			"port number"},
		{"name, no number",
			&networking.Port{
				Protocol: "http",
				Number:   0,
				Name:     "Henry",
			},
			""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateServerPort(tt.in)
			if err == nil && tt.out != "" {
				t.Fatalf("validateServerPort(%v) = nil, wanted %q", tt.in, tt.out)
			} else if err != nil && tt.out == "" {
				t.Fatalf("validateServerPort(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.out) {
				t.Fatalf("validateServerPort(%v) = %v, wanted %q", tt.in, err, tt.out)
			}
		})
	}
}

func TestValidateTlsOptions(t *testing.T) {
	tests := []struct {
		name string
		in   *networking.ServerTLSSettings
		out  string
	}{
		{"empty", &networking.ServerTLSSettings{}, ""},
		{"simple",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh"},
			""},
		{"simple with client bundle",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker"},
			""},
		{"simple sds with client bundle",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
				CredentialName:    "sds-name"},
			""},
		{"simple no server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
			},
			"server certificate"},
		{"simple no private key",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        ""},
			"private key"},
		{"simple sds no server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
				CredentialName:    "sds-name",
			},
			""},
		{"simple sds no private key",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "",
				CredentialName:    "sds-name",
			},
			""},
		{"mutual",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker"},
			""},
		{"mutual sds",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
				CredentialName:    "sds-name",
			},
			""},
		{"mutual no server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker"},
			"server certificate"},
		{"mutual sds no server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
				CredentialName:    "sds-name"},
			""},
		{"mutual no client CA bundle",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    ""},
			"client CA bundle"},
		// this pair asserts we get errors about both client and server certs missing when in mutual mode
		// and both are absent, but requires less rewriting of the testing harness than merging the cases
		{"mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    ""},
			"server certificate"},
		{"mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    ""},
			"private key"},
		{"mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    ""},
			"client CA bundle"},
		{"pass through sds no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_PASSTHROUGH,
				ServerCertificate: "",
				CaCertificates:    "",
				CredentialName:    "sds-name"},
			""},
		{"istio_mutual no certs",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_ISTIO_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    ""},
			""},
		{"istio_mutual with server cert",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_ISTIO_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard"},
			"cannot have associated server cert"},
		{"istio_mutual with client bundle",
			&networking.ServerTLSSettings{
				Mode:              networking.ServerTLSSettings_ISTIO_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker"},
			"cannot have associated"},
		{"istio_mutual with private key",
			&networking.ServerTLSSettings{
				Mode:       networking.ServerTLSSettings_ISTIO_MUTUAL,
				PrivateKey: "Khan Noonien Singh"},
			"cannot have associated private key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTLSOptions(tt.in)
			if err == nil && tt.out != "" {
				t.Fatalf("validateTlsOptions(%v) = nil, wanted %q", tt.in, tt.out)
			} else if err != nil && tt.out == "" {
				t.Fatalf("validateTlsOptions(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.out) {
				t.Fatalf("validateTlsOptions(%v) = %v, wanted %q", tt.in, err, tt.out)
			}
		})
	}
}

func TestValidateTLS(t *testing.T) {
	testCases := []struct {
		name  string
		tls   *networking.ClientTLSSettings
		valid bool
	}{
		{
			name: "SIMPLE: Credential Name set correctly",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_SIMPLE,
				CredentialName:    "some credential",
				ClientCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			valid: true,
		},
		{
			name: "SIMPLE CredentialName set with ClientCertificate specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_SIMPLE,
				CredentialName:    "credential",
				ClientCertificate: "cert",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			valid: false,
		},
		{
			name: "SIMPLE: CredentialName set with PrivateKey specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_SIMPLE,
				CredentialName:    "credential",
				ClientCertificate: "",
				PrivateKey:        "key",
				CaCertificates:    "",
			},
			valid: false,
		},
		{
			name: "SIMPLE: CredentialName set with CACertficiates specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_SIMPLE,
				CredentialName:    "credential",
				ClientCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "ca",
			},
			valid: false,
		},
		{
			name: "MUTUAL: Credential Name set correctly",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				CredentialName:    "some credential",
				ClientCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			valid: true,
		},
		{
			name: "MUTUAL CredentialName set with ClientCertificate specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				CredentialName:    "credential",
				ClientCertificate: "cert",
				PrivateKey:        "",
				CaCertificates:    "",
			},
			valid: false,
		},
		{
			name: "MUTUAL: CredentialName set with PrivateKey specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				CredentialName:    "credential",
				ClientCertificate: "",
				PrivateKey:        "key",
				CaCertificates:    "",
			},
			valid: false,
		},
		{
			name: "MUTUAL: CredentialName set with CACertficiates specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				CredentialName:    "credential",
				ClientCertificate: "",
				PrivateKey:        "",
				CaCertificates:    "ca",
			},
			valid: false,
		},
		{
			name: "MUTUAL: CredentialName not set with ClientCertificate and Key specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "cert",
				PrivateKey:        "key",
			},
			valid: true,
		},
		{
			name: "MUTUAL: CredentialName not set with ClientCertificate specified and Key missing",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "cert",
				PrivateKey:        "",
			},
			valid: false,
		},
		{
			name: "MUTUAL: CredentialName not set with ClientCertificate missing and Key specified",
			tls: &networking.ClientTLSSettings{
				Mode:              networking.ClientTLSSettings_MUTUAL,
				ClientCertificate: "",
				PrivateKey:        "key",
			},
			valid: false,
		},
	}

	for _, tc := range testCases {
		if got := validateTLS(tc.tls); (got == nil) != tc.valid {
			t.Errorf("ValidateTLS(%q) => got valid=%v, want valid=%v",
				tc.name, got == nil, tc.valid)
		}
	}
}

func TestValidateHTTPHeaderName(t *testing.T) {
	testCases := []struct {
		name  string
		valid bool
	}{
		{name: "header1", valid: true},
		{name: "X-Requested-With", valid: true},
		{name: "", valid: false},
	}

	for _, tc := range testCases {
		if got := ValidateHTTPHeaderName(tc.name); (got == nil) != tc.valid {
			t.Errorf("ValidateHTTPHeaderName(%q) => got valid=%v, want valid=%v",
				tc.name, got == nil, tc.valid)
		}
	}
}

func TestValidateCORSPolicy(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.CorsPolicy
		valid bool
	}{
		{name: "valid", in: &networking.CorsPolicy{
			AllowMethods:  []string{"GET", "POST"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{"header3"},
			MaxAge:        &types.Duration{Seconds: 2},
		}, valid: true},
		{name: "bad method", in: &networking.CorsPolicy{
			AllowMethods:  []string{"GET", "PUTT"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{"header3"},
			MaxAge:        &types.Duration{Seconds: 2},
		}, valid: false},
		{name: "bad header", in: &networking.CorsPolicy{
			AllowMethods:  []string{"GET", "POST"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{""},
			MaxAge:        &types.Duration{Seconds: 2},
		}, valid: false},
		{name: "bad max age", in: &networking.CorsPolicy{
			AllowMethods:  []string{"GET", "POST"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{"header3"},
			MaxAge:        &types.Duration{Seconds: 2, Nanos: 42},
		}, valid: false},
		{name: "empty matchType AllowOrigins", in: &networking.CorsPolicy{
			AllowOrigins: []*networking.StringMatch{
				{MatchType: &networking.StringMatch_Exact{Exact: ""}},
				{MatchType: &networking.StringMatch_Prefix{Prefix: ""}},
				{MatchType: &networking.StringMatch_Regex{Regex: ""}},
			},
			AllowMethods:  []string{"GET", "POST"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{"header3"},
			MaxAge:        &types.Duration{Seconds: 2},
		}, valid: false},
		{name: "non empty matchType AllowOrigins", in: &networking.CorsPolicy{
			AllowOrigins: []*networking.StringMatch{
				{MatchType: &networking.StringMatch_Exact{Exact: "exact"}},
				{MatchType: &networking.StringMatch_Prefix{Prefix: "prefix"}},
				{MatchType: &networking.StringMatch_Regex{Regex: "regex"}},
			},
			AllowMethods:  []string{"GET", "POST"},
			AllowHeaders:  []string{"header1", "header2"},
			ExposeHeaders: []string{"header3"},
			MaxAge:        &types.Duration{Seconds: 2},
		}, valid: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateCORSPolicy(tc.in); (got == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got == nil, tc.valid, got)
			}
		})
	}
}

func TestValidateHTTPStatus(t *testing.T) {
	testCases := []struct {
		in    int32
		valid bool
	}{
		{-100, false},
		{0, false},
		{200, true},
		{600, true},
		{601, false},
	}

	for _, tc := range testCases {
		if got := validateHTTPStatus(tc.in); (got == nil) != tc.valid {
			t.Errorf("validateHTTPStatus(%d) => got valid=%v, want valid=%v",
				tc.in, got, tc.valid)
		}
	}
}

func TestValidateHTTPFaultInjectionAbort(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.HTTPFaultInjection_Abort
		valid bool
	}{
		{name: "nil", in: nil, valid: true},
		{name: "valid", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 20,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: true},
		{name: "valid default", in: &networking.HTTPFaultInjection_Abort{
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: true},
		{name: "invalid http status", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 20,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 9000,
			},
		}, valid: false},
		{name: "invalid low http status", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 20,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 100,
			},
		}, valid: false},
		{name: "valid percentage", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: 0.001,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: true},
		{name: "invalid fractional percent", in: &networking.HTTPFaultInjection_Abort{
			Percentage: &networking.Percent{
				Value: -10.0,
			},
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateHTTPFaultInjectionAbort(tc.in); (got == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got == nil, tc.valid, got)
			}
		})
	}
}

func TestValidateHTTPFaultInjectionDelay(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.HTTPFaultInjection_Delay
		valid bool
	}{
		{name: "nil", in: nil, valid: true},
		{name: "valid fixed", in: &networking.HTTPFaultInjection_Delay{
			Percentage: &networking.Percent{
				Value: 20,
			},
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &types.Duration{Seconds: 3},
			},
		}, valid: true},
		{name: "valid default", in: &networking.HTTPFaultInjection_Delay{
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &types.Duration{Seconds: 3},
			},
		}, valid: true},
		{name: "invalid percent", in: &networking.HTTPFaultInjection_Delay{
			Percentage: &networking.Percent{
				Value: 101,
			},
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &types.Duration{Seconds: 3},
			},
		}, valid: false},
		{name: "invalid delay", in: &networking.HTTPFaultInjection_Delay{
			Percentage: &networking.Percent{
				Value: 20,
			},
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &types.Duration{Seconds: 3, Nanos: 42},
			},
		}, valid: false},
		{name: "valid fractional percentage", in: &networking.HTTPFaultInjection_Delay{
			Percentage: &networking.Percent{
				Value: 0.001,
			},
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &types.Duration{Seconds: 3},
			},
		}, valid: true},
		{name: "invalid fractional percentage", in: &networking.HTTPFaultInjection_Delay{
			Percentage: &networking.Percent{
				Value: -10.0,
			},
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &types.Duration{Seconds: 3},
			},
		}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateHTTPFaultInjectionDelay(tc.in); (got == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got == nil, tc.valid, got)
			}
		})
	}
}

func TestValidateHTTPRetry(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.HTTPRetry
		valid bool
	}{
		{name: "valid", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &types.Duration{Seconds: 2},
			RetryOn:       "5xx,gateway-error",
		}, valid: true},
		{name: "disable retries", in: &networking.HTTPRetry{
			Attempts: 0,
		}, valid: true},
		{name: "invalid, retry policy configured but attempts set to zero", in: &networking.HTTPRetry{
			Attempts:      0,
			PerTryTimeout: &types.Duration{Seconds: 2},
			RetryOn:       "5xx,gateway-error",
		}, valid: false},
		{name: "valid default", in: &networking.HTTPRetry{
			Attempts: 10,
		}, valid: true},
		{name: "valid http status retryOn", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &types.Duration{Seconds: 2},
			RetryOn:       "503,connect-failure",
		}, valid: true},
		{name: "invalid attempts", in: &networking.HTTPRetry{
			Attempts:      -1,
			PerTryTimeout: &types.Duration{Seconds: 2},
		}, valid: false},
		{name: "invalid timeout", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &types.Duration{Seconds: 2, Nanos: 1},
		}, valid: false},
		{name: "timeout too small", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &types.Duration{Nanos: 999},
		}, valid: false},
		{name: "invalid policy retryOn", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &types.Duration{Seconds: 2},
			RetryOn:       "5xx,invalid policy",
		}, valid: false},
		{name: "invalid http status retryOn", in: &networking.HTTPRetry{
			Attempts:      10,
			PerTryTimeout: &types.Duration{Seconds: 2},
			RetryOn:       "600,connect-failure",
		}, valid: false},
		{name: "invalid, retryRemoteLocalities configured but attempts set to zero", in: &networking.HTTPRetry{
			Attempts:              0,
			RetryRemoteLocalities: &types.BoolValue{Value: false},
		}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateHTTPRetry(tc.in); (got == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got == nil, tc.valid, got)
			}
		})
	}
}

func TestValidateHTTPRewrite(t *testing.T) {
	testCases := []struct {
		name  string
		in    *networking.HTTPRewrite
		valid bool
	}{
		{
			name:  "nil in",
			in:    nil,
			valid: true,
		},
		{
			name: "uri and authority",
			in: &networking.HTTPRewrite{
				Uri:       "/path/to/resource",
				Authority: "foobar.org",
			},
			valid: true,
		},
		{
			name: "uri",
			in: &networking.HTTPRewrite{
				Uri: "/path/to/resource",
			},
			valid: true,
		},
		{
			name: "authority",
			in: &networking.HTTPRewrite{
				Authority: "foobar.org",
			},
			valid: true,
		},
		{
			name:  "no uri or authority",
			in:    &networking.HTTPRewrite{},
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateHTTPRewrite(tc.in); (got == nil) != tc.valid {
				t.Errorf("got valid=%v, want valid=%v: %v",
					got == nil, tc.valid, got)
			}
		})
	}
}

func TestValidatePortName(t *testing.T) {
	testCases := []struct {
		name  string
		valid bool
	}{
		{
			name:  "",
			valid: false,
		},
		{
			name:  "simple",
			valid: true,
		},
		{
			name:  "full",
			valid: true,
		},
		{
			name:  "toolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolongtoolong",
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidatePortName(tc.name); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateHTTPRedirect(t *testing.T) {
	testCases := []struct {
		name     string
		redirect *networking.HTTPRedirect
		valid    bool
	}{
		{
			name:     "nil redirect",
			redirect: nil,
			valid:    true,
		},
		{
			name: "empty uri and authority",
			redirect: &networking.HTTPRedirect{
				Uri:       "",
				Authority: "",
			},
			valid: false,
		},
		{
			name: "too small redirect code",
			redirect: &networking.HTTPRedirect{
				Uri:          "t",
				Authority:    "",
				RedirectCode: 299,
			},
			valid: false,
		},
		{
			name: "too large redirect code",
			redirect: &networking.HTTPRedirect{
				Uri:          "t",
				Authority:    "",
				RedirectCode: 400,
			},
			valid: false,
		},
		{
			name: "empty authority",
			redirect: &networking.HTTPRedirect{
				Uri:       "t",
				Authority: "",
			},
			valid: true,
		},
		{
			name: "empty uri",
			redirect: &networking.HTTPRedirect{
				Uri:       "",
				Authority: "t",
			},
			valid: true,
		},
		{
			name: "empty redirect code",
			redirect: &networking.HTTPRedirect{
				Uri:          "t",
				Authority:    "t",
				RedirectCode: 0,
			},
			valid: true,
		},
		{
			name: "normal redirect",
			redirect: &networking.HTTPRedirect{
				Uri:          "t",
				Authority:    "t",
				RedirectCode: 308,
			},
			valid: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateHTTPRedirect(tc.redirect); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateDestination(t *testing.T) {
	testCases := []struct {
		name        string
		destination *networking.Destination
		valid       bool
	}{
		{
			name:        "empty",
			destination: &networking.Destination{}, // nothing
			valid:       false,
		},
		{
			name: "simple",
			destination: &networking.Destination{
				Host: "foo.bar",
			},
			valid: true,
		},
		{name: "full",
			destination: &networking.Destination{
				Host:   "foo.bar",
				Subset: "shiny",
				Port: &networking.PortSelector{
					Number: 5000,
				},
			},
			valid: true,
		},
		{name: "unnumbered-selector",
			destination: &networking.Destination{
				Host:   "foo.bar",
				Subset: "shiny",
				Port:   &networking.PortSelector{},
			},
			valid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateDestination(tc.destination); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateHTTPRoute(t *testing.T) {
	testCases := []struct {
		name  string
		route *networking.HTTPRoute
		valid bool
	}{
		{name: "empty", route: &networking.HTTPRoute{ // nothing
		}, valid:                                     false},
		{name: "simple", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
		}, valid: true},
		{name: "no destination", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: nil,
			}},
		}, valid: false},
		{name: "weighted", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      25,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      75,
			}},
		}, valid: true},
		{name: "total weight > 100", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      55,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      50,
			}},
		}, valid: false},
		{name: "total weight < 100", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      49,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      50,
			}},
		}, valid: false},
		{name: "simple redirect", route: &networking.HTTPRoute{
			Redirect: &networking.HTTPRedirect{
				Uri:       "/lerp",
				Authority: "foo.biz",
			},
		}, valid: true},
		{name: "conflicting redirect and route", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
			Redirect: &networking.HTTPRedirect{
				Uri:       "/lerp",
				Authority: "foo.biz",
			},
		}, valid: false},
		{name: "request response headers", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
		}, valid: true},
		{name: "valid headers", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"name": "",
						},
						Set: map[string]string{
							"name": "",
						},
						Remove: []string{
							"name",
						},
					},
					Response: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"name": "",
						},
						Set: map[string]string{
							"name": "",
						},
						Remove: []string{
							"name",
						},
					},
				},
			}},
		}, valid: true},
		{name: "empty header name - request add", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - request set", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Set: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - request remove", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Request: &networking.Headers_HeaderOperations{
						Remove: []string{
							"",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - response add", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Add: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - response set", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Set: map[string]string{
							"": "value",
						},
					},
				},
			}},
		}, valid: false},
		{name: "empty header name - response remove", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
				Headers: &networking.Headers{
					Response: &networking.Headers_HeaderOperations{
						Remove: []string{
							"",
						},
					},
				},
			}},
		}, valid: false},
		{name: "null header match", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{{
				Headers: map[string]*networking.StringMatch{
					"header": nil,
				},
			}},
		}, valid: false},
		{name: "nil match", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: nil,
		}, valid: true},
		{name: "match with nil element", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: true},
		{name: "invalid mirror percent", route: &networking.HTTPRoute{
			MirrorPercent: &types.UInt32Value{Value: 101},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "invalid mirror percentage", route: &networking.HTTPRoute{
			MirrorPercentage: &networking.Percent{
				Value: 101,
			},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: false},
		{name: "valid mirror percentage", route: &networking.HTTPRoute{
			MirrorPercentage: &networking.Percent{
				Value: 1,
			},
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.bar"},
			}},
			Match: []*networking.HTTPMatchRequest{nil},
		}, valid: true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateHTTPRoute(tc.route, false); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateRouteDestination(t *testing.T) {
	testCases := []struct {
		name   string
		routes []*networking.RouteDestination
		valid  bool
	}{
		{name: "simple", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz"},
		}}, valid: true},
		{name: "wildcard dash", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "*-foo.baz"},
		}}, valid: true},
		{name: "wildcard prefix", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "*foo.baz"},
		}}, valid: true},
		{name: "wildcard", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "*"},
		}}, valid: false},
		{name: "bad wildcard", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.*"},
		}}, valid: false},
		{name: "bad fqdn", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "default/baz"},
		}}, valid: false},
		{name: "no destination", routes: []*networking.RouteDestination{{
			Destination: nil,
		}}, valid: false},
		{name: "weighted", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      25,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      75,
		}}, valid: true},
		{name: "weight < 0", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      5,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      -1,
		}}, valid: false},
		{name: "total weight > 100", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      55,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      50,
		}}, valid: false},
		{name: "total weight < 100", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      49,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      50,
		}}, valid: false},
		{name: "total weight = 100", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      100,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      0,
		}}, valid: true},
		{name: "weight = 0", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      0,
		}}, valid: true},
		{name: "total weight = 0 with multi RouteDestination", routes: []*networking.RouteDestination{{
			Destination: &networking.Destination{Host: "foo.baz.south"},
			Weight:      0,
		}, {
			Destination: &networking.Destination{Host: "foo.baz.east"},
			Weight:      0,
		}}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateRouteDestinations(tc.routes); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

// TODO: add TCP test cases once it is implemented
func TestValidateVirtualService(t *testing.T) {
	testCases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{name: "simple", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true},
		{name: "duplicate hosts", in: &networking.VirtualService{
			Hosts: []string{"*.foo.bar", "*.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "with no destination", in: &networking.VirtualService{
			Hosts: []string{"*.foo.bar", "*.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{}},
			}},
		}, valid: false},
		{name: "destination with out hosts", in: &networking.VirtualService{
			Hosts: []string{"*.foo.bar", "*.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{},
				}},
			}},
		}, valid: false},
		{name: "no hosts", in: &networking.VirtualService{
			Hosts: nil,
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "bad host", in: &networking.VirtualService{
			Hosts: []string{"foo.ba!r"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "no tcp or http routing", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
		}, valid: false},
		{name: "bad gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"b@dgateway"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "FQDN for gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"gateway.example.com"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true},
		{name: "namespace/name for gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"ns1/gateway"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true},
		{name: "namespace/* for gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"ns1/*"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "*/name for gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"*/gateway"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "wildcard for mesh gateway", in: &networking.VirtualService{
			Hosts: []string{"*"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "wildcard for non-mesh gateway", in: &networking.VirtualService{
			Hosts:    []string{"*"},
			Gateways: []string{"somegateway"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true},
		{name: "missing tcp route", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Tcp: []*networking.TCPRoute{{
				Match: []*networking.L4MatchAttributes{
					{Port: 999},
				},
			}},
		}, valid: false},
		{name: "missing tls route", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Tls: []*networking.TLSRoute{{
				Match: []*networking.TLSMatchAttributes{
					{
						Port:     999,
						SniHosts: []string{"foo.bar"},
					},
				},
			}},
		}, valid: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateVirtualService("", "", tc.in); (err == nil) != tc.valid {
				t.Fatalf("got valid=%v but wanted valid=%v: %v", err == nil, tc.valid, err)
			}
		})
	}
}

func TestValidateDestinationRule(t *testing.T) {
	cases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{name: "simple destination rule", in: &networking.DestinationRule{
			Host: "reviews",
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: true},

		{name: "missing destination name", in: &networking.DestinationRule{
			Host: "",
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: false},

		{name: "missing subset name", in: &networking.DestinationRule{
			Host: "reviews",
			Subsets: []*networking.Subset{
				{Name: "", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: false},

		{name: "valid traffic policy, top level", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
					Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
				},
				OutlierDetection: &networking.OutlierDetection{
					MinHealthPercent: 20,
				},
			},
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: true},

		{name: "invalid traffic policy, top level", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{},
				OutlierDetection: &networking.OutlierDetection{
					MinHealthPercent: 20,
				},
			},
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"}},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: false},

		{name: "valid traffic policy, subset level", in: &networking.DestinationRule{
			Host: "reviews",
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"},
					TrafficPolicy: &networking.TrafficPolicy{
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{
								Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
							},
						},
						ConnectionPool: &networking.ConnectionPoolSettings{
							Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
							Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
						},
						OutlierDetection: &networking.OutlierDetection{
							MinHealthPercent: 20,
						},
					},
				},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: true},

		{name: "invalid traffic policy, subset level", in: &networking.DestinationRule{
			Host: "reviews",
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"},
					TrafficPolicy: &networking.TrafficPolicy{
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{
								Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
							},
						},
						ConnectionPool: &networking.ConnectionPoolSettings{},
						OutlierDetection: &networking.OutlierDetection{
							MinHealthPercent: 20,
						},
					},
				},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: false},

		{name: "valid traffic policy, both levels", in: &networking.DestinationRule{
			Host: "reviews",
			TrafficPolicy: &networking.TrafficPolicy{
				LoadBalancer: &networking.LoadBalancerSettings{
					LbPolicy: &networking.LoadBalancerSettings_Simple{
						Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
					},
				},
				ConnectionPool: &networking.ConnectionPoolSettings{
					Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
					Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
				},
				OutlierDetection: &networking.OutlierDetection{
					MinHealthPercent: 20,
				},
			},
			Subsets: []*networking.Subset{
				{Name: "v1", Labels: map[string]string{"version": "v1"},
					TrafficPolicy: &networking.TrafficPolicy{
						LoadBalancer: &networking.LoadBalancerSettings{
							LbPolicy: &networking.LoadBalancerSettings_Simple{
								Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
							},
						},
						ConnectionPool: &networking.ConnectionPoolSettings{
							Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
							Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
						},
						OutlierDetection: &networking.OutlierDetection{
							MinHealthPercent: 30,
						},
					},
				},
				{Name: "v2", Labels: map[string]string{"version": "v2"}},
			},
		}, valid: true},
	}
	for _, c := range cases {
		if got := ValidateDestinationRule(someName, someNamespace, c.in); (got == nil) != c.valid {
			t.Errorf("ValidateDestinationRule failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateTrafficPolicy(t *testing.T) {
	cases := []struct {
		name  string
		in    networking.TrafficPolicy
		valid bool
	}{
		{name: "valid traffic policy", in: networking.TrafficPolicy{
			LoadBalancer: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_Simple{
					Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
				},
			},
			ConnectionPool: &networking.ConnectionPoolSettings{
				Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
				Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
			},
			OutlierDetection: &networking.OutlierDetection{
				MinHealthPercent: 20,
			},
		},
			valid: true},
		{name: "invalid traffic policy, nil entries", in: networking.TrafficPolicy{},
			valid: false},

		{name: "invalid traffic policy, missing port in port level settings", in: networking.TrafficPolicy{
			PortLevelSettings: []*networking.TrafficPolicy_PortTrafficPolicy{
				{
					LoadBalancer: &networking.LoadBalancerSettings{
						LbPolicy: &networking.LoadBalancerSettings_Simple{
							Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
						},
					},
					ConnectionPool: &networking.ConnectionPoolSettings{
						Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
						Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
					},
					OutlierDetection: &networking.OutlierDetection{
						MinHealthPercent: 20,
					},
				},
			},
		},
			valid: false},
		{name: "invalid traffic policy, bad connection pool", in: networking.TrafficPolicy{
			LoadBalancer: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_Simple{
					Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
				},
			},
			ConnectionPool: &networking.ConnectionPoolSettings{},
			OutlierDetection: &networking.OutlierDetection{
				MinHealthPercent: 20,
			},
		},
			valid: false},
		{name: "invalid traffic policy, panic threshold too low", in: networking.TrafficPolicy{
			LoadBalancer: &networking.LoadBalancerSettings{
				LbPolicy: &networking.LoadBalancerSettings_Simple{
					Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
				},
			},
			ConnectionPool: &networking.ConnectionPoolSettings{
				Tcp:  &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: 7},
				Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: 11},
			},
			OutlierDetection: &networking.OutlierDetection{
				MinHealthPercent: -1,
			},
		},
			valid: false},
	}
	for _, c := range cases {
		if got := validateTrafficPolicy(&c.in); (got == nil) != c.valid {
			t.Errorf("ValidateTrafficPolicy failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateConnectionPool(t *testing.T) {
	cases := []struct {
		name  string
		in    networking.ConnectionPoolSettings
		valid bool
	}{
		{name: "valid connection pool, tcp and http", in: networking.ConnectionPoolSettings{
			Tcp: &networking.ConnectionPoolSettings_TCPSettings{
				MaxConnections: 7,
				ConnectTimeout: &types.Duration{Seconds: 2},
			},
			Http: &networking.ConnectionPoolSettings_HTTPSettings{
				Http1MaxPendingRequests:  2,
				Http2MaxRequests:         11,
				MaxRequestsPerConnection: 5,
				MaxRetries:               4,
				IdleTimeout:              &types.Duration{Seconds: 30},
			},
		},
			valid: true},

		{name: "valid connection pool, tcp only", in: networking.ConnectionPoolSettings{
			Tcp: &networking.ConnectionPoolSettings_TCPSettings{
				MaxConnections: 7,
				ConnectTimeout: &types.Duration{Seconds: 2},
			},
		},
			valid: true},

		{name: "valid connection pool, http only", in: networking.ConnectionPoolSettings{
			Http: &networking.ConnectionPoolSettings_HTTPSettings{
				Http1MaxPendingRequests:  2,
				Http2MaxRequests:         11,
				MaxRequestsPerConnection: 5,
				MaxRetries:               4,
				IdleTimeout:              &types.Duration{Seconds: 30},
			},
		},
			valid: true},

		{name: "valid connection pool, http only with empty idle timeout", in: networking.ConnectionPoolSettings{
			Http: &networking.ConnectionPoolSettings_HTTPSettings{
				Http1MaxPendingRequests:  2,
				Http2MaxRequests:         11,
				MaxRequestsPerConnection: 5,
				MaxRetries:               4,
			},
		},
			valid: true},

		{name: "invalid connection pool, empty", in: networking.ConnectionPoolSettings{}, valid: false},

		{name: "invalid connection pool, bad max connections", in: networking.ConnectionPoolSettings{
			Tcp: &networking.ConnectionPoolSettings_TCPSettings{MaxConnections: -1}},
			valid: false},

		{name: "invalid connection pool, bad connect timeout", in: networking.ConnectionPoolSettings{
			Tcp: &networking.ConnectionPoolSettings_TCPSettings{
				ConnectTimeout: &types.Duration{Seconds: 2, Nanos: 5}}},
			valid: false},

		{name: "invalid connection pool, bad max pending requests", in: networking.ConnectionPoolSettings{
			Http: &networking.ConnectionPoolSettings_HTTPSettings{Http1MaxPendingRequests: -1}},
			valid: false},

		{name: "invalid connection pool, bad max requests", in: networking.ConnectionPoolSettings{
			Http: &networking.ConnectionPoolSettings_HTTPSettings{Http2MaxRequests: -1}},
			valid: false},

		{name: "invalid connection pool, bad max requests per connection", in: networking.ConnectionPoolSettings{
			Http: &networking.ConnectionPoolSettings_HTTPSettings{MaxRequestsPerConnection: -1}},
			valid: false},

		{name: "invalid connection pool, bad max retries", in: networking.ConnectionPoolSettings{
			Http: &networking.ConnectionPoolSettings_HTTPSettings{MaxRetries: -1}},
			valid: false},

		{name: "invalid connection pool, bad idle timeout", in: networking.ConnectionPoolSettings{
			Http: &networking.ConnectionPoolSettings_HTTPSettings{IdleTimeout: &types.Duration{Seconds: 30, Nanos: 5}}},
			valid: false},
	}

	for _, c := range cases {
		if got := validateConnectionPool(&c.in); (got == nil) != c.valid {
			t.Errorf("ValidateConnectionSettings failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateLoadBalancer(t *testing.T) {
	duration := types.Duration{Seconds: int64(time.Hour / time.Second)}
	cases := []struct {
		name  string
		in    networking.LoadBalancerSettings
		valid bool
	}{
		{name: "valid load balancer with simple load balancing", in: networking.LoadBalancerSettings{
			LbPolicy: &networking.LoadBalancerSettings_Simple{
				Simple: networking.LoadBalancerSettings_ROUND_ROBIN,
			},
		},
			valid: true},

		{name: "valid load balancer with consistentHash load balancing", in: networking.LoadBalancerSettings{
			LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
				ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
					MinimumRingSize: 1024,
					HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
						HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
							Name: "test",
							Ttl:  &duration,
						},
					},
				},
			},
		},
			valid: true},

		{name: "invalid load balancer with consistentHash load balancing, missing ttl", in: networking.LoadBalancerSettings{
			LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
				ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
					MinimumRingSize: 1024,
					HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
						HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
							Name: "test",
						},
					},
				},
			},
		},
			valid: false},

		{name: "invalid load balancer with consistentHash load balancing, missing name", in: networking.LoadBalancerSettings{
			LbPolicy: &networking.LoadBalancerSettings_ConsistentHash{
				ConsistentHash: &networking.LoadBalancerSettings_ConsistentHashLB{
					MinimumRingSize: 1024,
					HashKey: &networking.LoadBalancerSettings_ConsistentHashLB_HttpCookie{
						HttpCookie: &networking.LoadBalancerSettings_ConsistentHashLB_HTTPCookie{
							Ttl: &duration,
						},
					},
				},
			},
		},
			valid: false},
	}

	for _, c := range cases {
		if got := validateLoadBalancer(&c.in); (got == nil) != c.valid {
			t.Errorf("validateLoadBalancer failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateOutlierDetection(t *testing.T) {
	cases := []struct {
		name  string
		in    networking.OutlierDetection
		valid bool
	}{
		{name: "valid outlier detection", in: networking.OutlierDetection{
			Interval:           &types.Duration{Seconds: 2},
			BaseEjectionTime:   &types.Duration{Seconds: 2},
			MaxEjectionPercent: 50,
		}, valid: true},

		{name: "invalid outlier detection, bad interval", in: networking.OutlierDetection{
			Interval: &types.Duration{Seconds: 2, Nanos: 5}},
			valid: false},

		{name: "invalid outlier detection, bad base ejection time", in: networking.OutlierDetection{
			BaseEjectionTime: &types.Duration{Seconds: 2, Nanos: 5}},
			valid: false},

		{name: "invalid outlier detection, bad max ejection percent", in: networking.OutlierDetection{
			MaxEjectionPercent: 105},
			valid: false},
		{name: "invalid outlier detection, panic threshold too low", in: networking.OutlierDetection{
			MinHealthPercent: -1,
		},
			valid: false},
		{name: "invalid outlier detection, panic threshold too high", in: networking.OutlierDetection{
			MinHealthPercent: 101,
		},
			valid: false},
	}

	for _, c := range cases {
		if got := validateOutlierDetection(&c.in); (got == nil) != c.valid {
			t.Errorf("ValidateOutlierDetection failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateEnvoyFilter(t *testing.T) {
	tests := []struct {
		name  string
		in    proto.Message
		error string
	}{
		{name: "empty filters", in: &networking.EnvoyFilter{}, error: ""},

		{name: "invalid applyTo", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: 0,
				},
			},
		}, error: "Envoy filter: missing applyTo"},
		{name: "nil patch", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Patch:   nil,
				},
			},
		}, error: "Envoy filter: missing patch"},
		{name: "invalid patch operation", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Patch:   &networking.EnvoyFilter_Patch{},
				},
			},
		}, error: "Envoy filter: missing patch operation"},
		{name: "nil patch value", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_ADD,
					},
				},
			},
		}, error: "Envoy filter: missing patch value for non-remove operation"},
		{name: "match with invalid regex", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						Proxy: &networking.EnvoyFilter_ProxyMatch{
							ProxyVersion: "%#@~++==`24c234`",
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: invalid regex for proxy version, [error parsing regexp: invalid nested repetition operator: `++`]"},
		{name: "match with valid regex", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						Proxy: &networking.EnvoyFilter_ProxyMatch{
							ProxyVersion: `release-1\.2-23434`,
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: ""},
		{name: "listener with invalid match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: applyTo for listener class objects cannot have non listener match"},
		{name: "listener with invalid filter match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Sni:    "124",
									Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{},
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: filter match has no name to match on"},
		{name: "listener with sub filter match and invalid applyTo", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_LISTENER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
										Name:      "random",
										SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{},
									},
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: subfilter match can be used with applyTo HTTP_FILTER only"},
		{name: "listener with sub filter match and invalid filter name", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
										Name:      "random",
										SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{},
									},
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: subfilter match requires filter match with envoy.filters.network.http_connection_manager"},
		{name: "listener with sub filter match and no sub filter name", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_HTTP_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Filter: &networking.EnvoyFilter_ListenerMatch_FilterMatch{
										Name:      wellknown.HTTPConnectionManager,
										SubFilter: &networking.EnvoyFilter_ListenerMatch_SubFilterMatch{},
									},
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: subfilter match has no name to match on"},
		{name: "route configuration with invalid match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_VIRTUAL_HOST,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: applyTo for http route class objects cannot have non route configuration match"},
		{name: "cluster with invalid match", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_CLUSTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_REMOVE,
					},
				},
			},
		}, error: "Envoy filter: applyTo for cluster class objects cannot have non cluster match"},
		{name: "invalid patch value", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_CLUSTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_ADD,
						Value: &types.Struct{
							Fields: map[string]*types.Value{
								"name": {
									Kind: &types.Value_BoolValue{BoolValue: false},
								},
							},
						},
					},
				},
			},
		}, error: `Envoy filter: json: cannot unmarshal bool into Go value of type string`},
		{name: "happy config", in: &networking.EnvoyFilter{
			ConfigPatches: []*networking.EnvoyFilter_EnvoyConfigObjectPatch{
				{
					ApplyTo: networking.EnvoyFilter_CLUSTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
							Cluster: &networking.EnvoyFilter_ClusterMatch{},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_ADD,
						Value: &types.Struct{
							Fields: map[string]*types.Value{
								"lb_policy": {
									Kind: &types.Value_StringValue{StringValue: "RING_HASH"},
								},
							},
						},
					},
				},
				{
					ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Name: "envoy.tcp_proxy",
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_INSERT_BEFORE,
						Value: &types.Struct{
							Fields: map[string]*types.Value{
								"typed_config": {
									Kind: &types.Value_StructValue{StructValue: &types.Struct{
										Fields: map[string]*types.Value{
											"@type": {
												Kind: &types.Value_StringValue{
													StringValue: "type.googleapis.com/envoy.config.filter.network.ext_authz.v2.ExtAuthz",
												},
											},
										},
									}},
								},
							},
						},
					},
				},
				{
					ApplyTo: networking.EnvoyFilter_NETWORK_FILTER,
					Match: &networking.EnvoyFilter_EnvoyConfigObjectMatch{
						ObjectTypes: &networking.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
							Listener: &networking.EnvoyFilter_ListenerMatch{
								FilterChain: &networking.EnvoyFilter_ListenerMatch_FilterChainMatch{
									Name: "envoy.tcp_proxy",
								},
							},
						},
					},
					Patch: &networking.EnvoyFilter_Patch{
						Operation: networking.EnvoyFilter_Patch_INSERT_FIRST,
						Value: &types.Struct{
							Fields: map[string]*types.Value{
								"typed_config": {
									Kind: &types.Value_StructValue{StructValue: &types.Struct{
										Fields: map[string]*types.Value{
											"@type": {
												Kind: &types.Value_StringValue{
													StringValue: "type.googleapis.com/envoy.config.filter.network.ext_authz.v2.ExtAuthz",
												},
											},
										},
									}},
								},
							},
						},
					},
				},
			},
		}, error: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEnvoyFilter(someName, someNamespace, tt.in)
			if err == nil && tt.error != "" {
				t.Fatalf("ValidateEnvoyFilter(%v) = nil, wanted %q", tt.in, tt.error)
			} else if err != nil && tt.error == "" {
				t.Fatalf("ValidateEnvoyFilter(%v) = %v, wanted nil", tt.in, err)
			} else if err != nil && !strings.Contains(err.Error(), tt.error) {
				t.Fatalf("ValidateEnvoyFilter(%v) = %v, wanted %q", tt.in, err, tt.error)
			}
		})
	}
}

func TestValidateServiceEntries(t *testing.T) {
	cases := []struct {
		name  string
		in    networking.ServiceEntry
		valid bool
	}{
		{name: "discovery type DNS", in: networking.ServiceEntry{
			Hosts: []string{"*.google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}},
				{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: true},

		{name: "discovery type DNS, label tlsMode: istio", in: networking.ServiceEntry{
			Hosts: []string{"*.google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}, Labels: map[string]string{"security.istio.io/tlsMode": "istio"}},
				{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}, Labels: map[string]string{"security.istio.io/tlsMode": "istio"}},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: true},

		{name: "discovery type DNS, IP in endpoints", in: networking.ServiceEntry{
			Hosts: []string{"*.google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "1.1.1.1", Ports: map[string]uint32{"http-valid1": 8080}},
				{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: true},

		{name: "empty hosts", in: networking.ServiceEntry{
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: false},

		{name: "bad hosts", in: networking.ServiceEntry{
			Hosts: []string{"-"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: false},
		{name: "full wildcard host", in: networking.ServiceEntry{
			Hosts: []string{"foo.com", "*"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: false},
		{name: "short name host", in: networking.ServiceEntry{
			Hosts: []string{"foo", "bar.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "in.google.com", Ports: map[string]uint32{"http-valid1": 9080}},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: true},
		{name: "undefined endpoint port", in: networking.ServiceEntry{
			Hosts: []string{"google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 80, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}},
				{Address: "in.google.com", Ports: map[string]uint32{"http-dne": 9080}},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: false},

		{name: "discovery type DNS, non-FQDN endpoint", in: networking.ServiceEntry{
			Hosts: []string{"*.google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "*.lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}},
				{Address: "in.google.com", Ports: map[string]uint32{"http-dne": 9080}},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: false},

		{name: "discovery type DNS, non-FQDN host", in: networking.ServiceEntry{
			Hosts: []string{"*.google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},

			Resolution: networking.ServiceEntry_DNS,
		},
			valid: false},

		{name: "discovery type DNS, no endpoints", in: networking.ServiceEntry{
			Hosts: []string{"google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},

			Resolution: networking.ServiceEntry_DNS,
		},
			valid: true},

		{name: "discovery type DNS, unix endpoint", in: networking.ServiceEntry{
			Hosts: []string{"*.google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "unix:///lon/google/com"},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: false},

		{name: "discovery type none", in: networking.ServiceEntry{
			Hosts: []string{"google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Resolution: networking.ServiceEntry_NONE,
		},
			valid: true},

		{name: "discovery type none, endpoints provided", in: networking.ServiceEntry{
			Hosts: []string{"google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}},
			},
			Resolution: networking.ServiceEntry_NONE,
		},
			valid: false},

		{name: "discovery type none, cidr addresses", in: networking.ServiceEntry{
			Hosts:     []string{"google.com"},
			Addresses: []string{"172.1.2.16/16"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Resolution: networking.ServiceEntry_NONE,
		},
			valid: true},

		{name: "discovery type static, cidr addresses with endpoints", in: networking.ServiceEntry{
			Hosts:     []string{"google.com"},
			Addresses: []string{"172.1.2.16/16"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "1.1.1.1", Ports: map[string]uint32{"http-valid1": 8080}},
				{Address: "2.2.2.2", Ports: map[string]uint32{"http-valid2": 9080}},
			},
			Resolution: networking.ServiceEntry_STATIC,
		},
			valid: true},

		{name: "discovery type static", in: networking.ServiceEntry{
			Hosts:     []string{"google.com"},
			Addresses: []string{"172.1.2.16"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "1.1.1.1", Ports: map[string]uint32{"http-valid1": 8080}},
				{Address: "2.2.2.2", Ports: map[string]uint32{"http-valid2": 9080}},
			},
			Resolution: networking.ServiceEntry_STATIC,
		},
			valid: true},

		{name: "discovery type static, FQDN in endpoints", in: networking.ServiceEntry{
			Hosts:     []string{"google.com"},
			Addresses: []string{"172.1.2.16"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "google.com", Ports: map[string]uint32{"http-valid1": 8080}},
				{Address: "2.2.2.2", Ports: map[string]uint32{"http-valid2": 9080}},
			},
			Resolution: networking.ServiceEntry_STATIC,
		},
			valid: false},

		{name: "discovery type static, missing endpoints", in: networking.ServiceEntry{
			Hosts:     []string{"google.com"},
			Addresses: []string{"172.1.2.16"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Resolution: networking.ServiceEntry_STATIC,
		},
			valid: true},

		{name: "discovery type static, bad endpoint port name", in: networking.ServiceEntry{
			Hosts:     []string{"google.com"},
			Addresses: []string{"172.1.2.16"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "1.1.1.1", Ports: map[string]uint32{"http-valid1": 8080}},
				{Address: "2.2.2.2", Ports: map[string]uint32{"http-dne": 9080}},
			},
			Resolution: networking.ServiceEntry_STATIC,
		},
			valid: false},

		{name: "discovery type none, conflicting port names", in: networking.ServiceEntry{
			Hosts: []string{"google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-conflict"},
				{Number: 8080, Protocol: "http", Name: "http-conflict"},
			},
			Resolution: networking.ServiceEntry_NONE,
		},
			valid: false},

		{name: "discovery type none, conflicting port numbers", in: networking.ServiceEntry{
			Hosts: []string{"google.com"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-conflict1"},
				{Number: 80, Protocol: "http", Name: "http-conflict2"},
			},
			Resolution: networking.ServiceEntry_NONE,
		},
			valid: false},

		{name: "unix socket", in: networking.ServiceEntry{
			Hosts: []string{"uds.cluster.local"},
			Ports: []*networking.Port{
				{Number: 6553, Protocol: "grpc", Name: "grpc-service1"},
			},
			Resolution: networking.ServiceEntry_STATIC,
			Endpoints: []*networking.WorkloadEntry{
				{Address: "unix:///path/to/socket"},
			},
		},
			valid: true},

		{name: "unix socket, relative path", in: networking.ServiceEntry{
			Hosts: []string{"uds.cluster.local"},
			Ports: []*networking.Port{
				{Number: 6553, Protocol: "grpc", Name: "grpc-service1"},
			},
			Resolution: networking.ServiceEntry_STATIC,
			Endpoints: []*networking.WorkloadEntry{
				{Address: "unix://./relative/path.sock"},
			},
		},
			valid: false},

		{name: "unix socket, endpoint ports", in: networking.ServiceEntry{
			Hosts: []string{"uds.cluster.local"},
			Ports: []*networking.Port{
				{Number: 6553, Protocol: "grpc", Name: "grpc-service1"},
			},
			Resolution: networking.ServiceEntry_STATIC,
			Endpoints: []*networking.WorkloadEntry{
				{Address: "unix:///path/to/socket", Ports: map[string]uint32{"grpc-service1": 6553}},
			},
		},
			valid: false},

		{name: "unix socket, multiple service ports", in: networking.ServiceEntry{
			Hosts: []string{"uds.cluster.local"},
			Ports: []*networking.Port{
				{Number: 6553, Protocol: "grpc", Name: "grpc-service1"},
				{Number: 80, Protocol: "http", Name: "http-service2"},
			},
			Resolution: networking.ServiceEntry_STATIC,
			Endpoints: []*networking.WorkloadEntry{
				{Address: "unix:///path/to/socket"},
			},
		},
			valid: false},
		{name: "empty protocol", in: networking.ServiceEntry{
			Hosts:     []string{"google.com"},
			Addresses: []string{"172.1.2.16/16"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Name: "http-valid2"},
			},
			Resolution: networking.ServiceEntry_NONE,
		},
			valid: true},
		{name: "selector", in: networking.ServiceEntry{
			Hosts:            []string{"google.com"},
			WorkloadSelector: &networking.WorkloadSelector{Labels: map[string]string{"foo": "bar"}},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
			},
		},
			valid: true},
		{name: "selector and endpoints", in: networking.ServiceEntry{
			Hosts:            []string{"google.com"},
			WorkloadSelector: &networking.WorkloadSelector{Labels: map[string]string{"foo": "bar"}},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
			},
			Endpoints: []*networking.WorkloadEntry{
				{Address: "1.1.1.1"},
			},
		},
			valid: false},
		{name: "bad selector key", in: networking.ServiceEntry{
			Hosts:            []string{"google.com"},
			WorkloadSelector: &networking.WorkloadSelector{Labels: map[string]string{"": "bar"}},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
			},
		},
			valid: false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateServiceEntry(someName, someNamespace, &c.in); (got == nil) != c.valid {
				t.Errorf("ValidateServiceEntry got valid=%v but wanted valid=%v: %v",
					got == nil, c.valid, got)
			}
		})
	}
}

func TestValidateAuthorizationPolicy(t *testing.T) {
	cases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{
			name: "good",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app":     "httpbin",
						"version": "v1",
					},
				},
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"sa1"},
								},
							},
							{
								Source: &security_beta.Source{
									Principals: []string{"sa2"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Methods: []string{"GET"},
								},
							},
							{
								Operation: &security_beta.Operation{
									Methods: []string{"POST"},
								},
							},
						},
						When: []*security_beta.Condition{
							{
								Key:    "source.ip",
								Values: []string{"1.2.3.4", "5.6.7.0/24"},
							},
							{
								Key:    "request.headers[:authority]",
								Values: []string{"v1", "v2"},
							},
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "allow-rules-nil",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_ALLOW,
			},
			valid: true,
		},
		{
			name: "deny-rules-nil",
			in: &security_beta.AuthorizationPolicy{
				Action: security_beta.AuthorizationPolicy_DENY,
			},
			valid: false,
		},
		{
			name: "selector-empty-value",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app":     "",
						"version": "v1",
					},
				},
			},
			valid: true,
		},
		{
			name: "selector-empty-key",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
						"":    "v1",
					},
				},
			},
			valid: false,
		},
		{
			name: "selector-wildcard-value",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin-*",
					},
				},
			},
			valid: false,
		},
		{
			name: "selector-wildcard-key",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app-*": "httpbin",
					},
				},
			},
			valid: false,
		},
		{
			name: "from-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{},
					},
				},
			},
			valid: false,
		},
		{
			name: "source-nil",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "source-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "to-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{},
					},
				},
			},
			valid: false,
		},
		{
			name: "operation-nil",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "operation-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Principals-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Principals: []string{"p1", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotPrincipals-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									NotPrincipals: []string{"p1", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "RequestPrincipals-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									RequestPrincipals: []string{"p1", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotRequestPrincipals-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									NotRequestPrincipals: []string{"p1", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Namespaces-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									Namespaces: []string{"ns", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotNamespaces-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									NotNamespaces: []string{"ns", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "IpBlocks-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									IpBlocks: []string{"1.2.3.4", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotIpBlocks-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									NotIpBlocks: []string{"1.2.3.4", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Hosts-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Hosts: []string{"host", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotHosts-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									NotHosts: []string{"host", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Ports-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports: []string{"80", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotPorts-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									NotPorts: []string{"80", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Methods-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Methods: []string{"GET", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotMethods-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									NotMethods: []string{"GET", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "Paths-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Paths: []string{"/path", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "NotPaths-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									NotPaths: []string{"/path", ""},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "value-empty",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key:    "request.headers[:authority]",
								Values: []string{"v1", ""},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid ip and port",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						From: []*security_beta.Rule_From{
							{
								Source: &security_beta.Source{
									IpBlocks:    []string{"1.2.3.4", "ip1"},
									NotIpBlocks: []string{"5.6.7.8", "ip2"},
								},
							},
						},
						To: []*security_beta.Rule_To{
							{
								Operation: &security_beta.Operation{
									Ports:    []string{"80", "port1"},
									NotPorts: []string{"90", "port2"},
								},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-key-missing",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Values: []string{"v1", "v2"},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-key-empty",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key:    "",
								Values: []string{"v1", "v2"},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-value-missing",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key: "source.principal",
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-value-invalid",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key:    "source.ip",
								Values: []string{"a.b.c.d"},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-notValue-invalid",
			in: &security_beta.AuthorizationPolicy{
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key:       "source.ip",
								NotValues: []string{"a.b.c.d"},
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "condition-unknown",
			in: &security_beta.AuthorizationPolicy{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				Rules: []*security_beta.Rule{
					{
						When: []*security_beta.Condition{
							{
								Key:    "key1",
								Values: []string{"v1"},
							},
						},
					},
				},
			},
			valid: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateAuthorizationPolicy("", "", c.in); (got == nil) != c.valid {
				t.Errorf("got: %v\nwant: %v", got, c.valid)
			}
		})
	}
}

func TestValidateMixerService(t *testing.T) {
	cases := []struct {
		name  string
		in    *mccpb.IstioService
		valid bool
	}{
		{
			name: "no name and service",
			in:   &mccpb.IstioService{},
		},
		{
			name: "specify both name and service",
			in:   &mccpb.IstioService{Service: "test-service-service", Name: "test-service-name"},
		},
		{
			name: "specify both namespace and service",
			in:   &mccpb.IstioService{Service: "test-service-service", Namespace: "test-service-namespace"},
		},
		{
			name: "specify both domain and service",
			in:   &mccpb.IstioService{Service: "test-service-service", Domain: "test-service-domain"},
		},
		{
			name: "invalid name label",
			in:   &mccpb.IstioService{Name: strings.Repeat("x", 64)},
		},
		{
			name: "invalid namespace label",
			in:   &mccpb.IstioService{Name: "test-service-name", Namespace: strings.Repeat("x", 64)},
		},
		{
			name: "invalid domain or labels",
			in:   &mccpb.IstioService{Name: "test-service-name", Domain: strings.Repeat("x", 256)},
		},
		{
			name:  "valid",
			in:    validService,
			valid: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateMixerService(c.in); (got == nil) != c.valid {
				t.Errorf("ValidateMixerService(%v): got(%v) != want(%v): %v", c.name, got == nil, c.valid, got)
			}
		})
	}
}

func TestValidateSidecar(t *testing.T) {
	tests := []struct {
		name  string
		in    *networking.Sidecar
		valid bool
	}{
		{"empty ingress and egress", &networking.Sidecar{}, false},
		{"default", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true},
		{"import local namespace with wildcard", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"./*"},
				},
			},
		}, true},
		{"import local namespace with fqdn", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"./foo.com"},
				},
			},
		}, true},
		{"import nothing", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"~/*"},
				},
			},
		}, true},
		{"bad egress host 1", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*"},
				},
			},
		}, false},
		{"bad egress host 2", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"/"},
				},
			},
		}, false},
		{"empty egress host", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{},
				},
			},
		}, false},
		{"multiple wildcard egress", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{
						"*/foo.com",
					},
				},
				{
					Hosts: []string{
						"ns1/bar.com",
					},
				},
			},
		}, false},
		{"wildcard egress not in end", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{
						"*/foo.com",
					},
				},
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   8080,
						Name:     "h8080",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
				},
			},
		}, false},
		{"invalid Port", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Protocol: "http1",
						Number:   1000000,
						Name:     "",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
				},
			},
		}, false},
		{"Port without name", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   8080,
					},
					Hosts: []string{
						"ns1/bar.com",
					},
				},
			},
		}, true},
		{"UDS bind in outbound", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind: "unix:///@foo/bar/com",
				},
			},
		}, true},
		{"UDS bind in inbound", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Bind:            "unix:///@foo/bar/com",
					DefaultEndpoint: "127.0.0.1:9999",
				},
			},
		}, false},
		{"UDS bind in outbound 2", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind: "unix:///foo/bar/com",
				},
			},
		}, true},
		{"invalid bind", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind: "foobar:///@foo/bar/com",
				},
			},
		}, false},
		{"invalid capture mode with uds bind", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind:        "unix:///@foo/bar/com",
					CaptureMode: networking.CaptureMode_IPTABLES,
				},
			},
		}, false},
		{"duplicate UDS bind", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind: "unix:///@foo/bar/com",
				},
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   0,
						Name:     "uds",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
					Bind: "unix:///@foo/bar/com",
				},
			},
		}, false},
		{"duplicate ports", &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					Hosts: []string{
						"ns1/bar.com",
					},
				},
				{
					Port: &networking.Port{
						Protocol: "tcp",
						Number:   90,
						Name:     "tcp",
					},
					Hosts: []string{
						"ns2/bar.com",
					},
				},
			},
		}, false},
		{"ingress without port", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					DefaultEndpoint: "127.0.0.1:110",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false},
		{"ingress with duplicate ports", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:110",
				},
				{
					Port: &networking.Port{
						Protocol: "tcp",
						Number:   90,
						Name:     "bar",
					},
					DefaultEndpoint: "127.0.0.1:110",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false},
		{"ingress without default endpoint", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false},
		{"ingress with invalid default endpoint IP", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "1.1.1.1:90",
				},
			},
		}, false},
		{"ingress with invalid default endpoint uds", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "unix:///",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false},
		{"ingress with invalid default endpoint port", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:hi",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false},
		{"valid ingress and egress", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true},
		{"valid ingress and empty egress", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Protocol: "http",
						Number:   90,
						Name:     "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
				},
			},
		}, false},
		{"empty protocol", &networking.Sidecar{
			Ingress: []*networking.IstioIngressListener{
				{
					Port: &networking.Port{
						Number: 90,
						Name:   "foo",
					},
					DefaultEndpoint: "127.0.0.1:9999",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true},
		{"ALLOW_ANY sidecar egress policy with no egress proxy ", &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true},
		{"sidecar egress proxy with RESGISTRY_ONLY(default)", &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				EgressProxy: &networking.Destination{
					Host:   "foo.bar",
					Subset: "shiny",
					Port: &networking.PortSelector{
						Number: 5000,
					},
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false},
		{"sidecar egress proxy with ALLOW_ANY", &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
				EgressProxy: &networking.Destination{
					Host:   "foo.bar",
					Subset: "shiny",
					Port: &networking.PortSelector{
						Number: 5000,
					},
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, true},
		{"sidecar egress proxy with ALLOW_ANY, service hostname invalid fqdn", &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
				EgressProxy: &networking.Destination{
					Host:   "foobar*123",
					Subset: "shiny",
					Port: &networking.PortSelector{
						Number: 5000,
					},
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false},
		{"sidecar egress proxy(without Port) with ALLOW_ANY", &networking.Sidecar{
			OutboundTrafficPolicy: &networking.OutboundTrafficPolicy{
				Mode: networking.OutboundTrafficPolicy_ALLOW_ANY,
				EgressProxy: &networking.Destination{
					Host:   "foo.bar",
					Subset: "shiny",
				},
			},
			Egress: []*networking.IstioEgressListener{
				{
					Hosts: []string{"*/*"},
				},
			},
		}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSidecar("foo", "bar", tt.in)
			if err == nil && !tt.valid {
				t.Fatalf("ValidateSidecar(%v) = true, wanted false", tt.in)
			} else if err != nil && tt.valid {
				t.Fatalf("ValidateSidecar(%v) = %v, wanted true", tt.in, err)
			}
		})
	}
}

func TestValidateLocalityLbSetting(t *testing.T) {
	cases := []struct {
		name  string
		in    *networking.LocalityLoadBalancerSetting
		valid bool
	}{
		{
			name:  "valid mesh config without LocalityLoadBalancerSetting",
			in:    nil,
			valid: true,
		},

		{
			name: "invalid LocalityLoadBalancerSetting_Distribute total weight > 100",
			in: &networking.LocalityLoadBalancerSetting{
				Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
					{
						From: "a/b/c",
						To: map[string]uint32{
							"a/b/c": 80,
							"a/b1":  25,
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid LocalityLoadBalancerSetting_Distribute total weight < 100",
			in: &networking.LocalityLoadBalancerSetting{
				Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
					{
						From: "a/b/c",
						To: map[string]uint32{
							"a/b/c": 80,
							"a/b1":  15,
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid LocalityLoadBalancerSetting_Distribute weight = 0",
			in: &networking.LocalityLoadBalancerSetting{
				Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
					{
						From: "a/b/c",
						To: map[string]uint32{
							"a/b/c": 0,
							"a/b1":  100,
						},
					},
				},
			},
			valid: false,
		},
		{
			name: "invalid LocalityLoadBalancerSetting specify both distribute and failover",
			in: &networking.LocalityLoadBalancerSetting{
				Distribute: []*networking.LocalityLoadBalancerSetting_Distribute{
					{
						From: "a/b/c",
						To: map[string]uint32{
							"a/b/c": 80,
							"a/b1":  20,
						},
					},
				},
				Failover: []*networking.LocalityLoadBalancerSetting_Failover{
					{
						From: "region1",
						To:   "region2",
					},
				},
			},
			valid: false,
		},

		{
			name: "invalid failover src and dst have same region",
			in: &networking.LocalityLoadBalancerSetting{
				Failover: []*networking.LocalityLoadBalancerSetting_Failover{
					{
						From: "region1",
						To:   "region1",
					},
				},
			},
			valid: false,
		},
	}

	for _, c := range cases {
		if got := validateLocalityLbSetting(c.in); (got == nil) != c.valid {
			t.Errorf("ValidateLocalityLbSetting failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateLocalities(t *testing.T) {
	cases := []struct {
		name       string
		localities []string
		valid      bool
	}{
		{
			name:       "multi wildcard locality",
			localities: []string{"*/zone/*"},
			valid:      false,
		},
		{
			name:       "wildcard not in suffix",
			localities: []string{"*/zone"},
			valid:      false,
		},
		{
			name:       "explicit wildcard region overlap",
			localities: []string{"*", "a/b/c"},
			valid:      false,
		},
		{
			name:       "implicit wildcard region overlap",
			localities: []string{"a", "a/b/c"},
			valid:      false,
		},
		{
			name:       "explicit wildcard zone overlap",
			localities: []string{"a/*", "a/b/c"},
			valid:      false,
		},
		{
			name:       "implicit wildcard zone overlap",
			localities: []string{"a/b", "a/b/c"},
			valid:      false,
		},
		{
			name:       "explicit wildcard subzone overlap",
			localities: []string{"a/b/*", "a/b/c"},
			valid:      false,
		},
		{
			name:       "implicit wildcard subzone overlap",
			localities: []string{"a/b", "a/b/c"},
			valid:      false,
		},
		{
			name:       "valid localities",
			localities: []string{"a1/*", "a2/*", "a3/b3/c3", "a4/b4", "a5/b5/*"},
			valid:      true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateLocalities(c.localities)
			if !c.valid && err == nil {
				t.Errorf("expect invalid localities")
			}

			if c.valid && err != nil {
				t.Errorf("expect valid localities. but got err %v", err)
			}
		})
	}
}

func TestValidationIPAddress(t *testing.T) {
	tests := []struct {
		name string
		addr string
		ok   bool
	}{
		{
			name: "valid ipv4 address",
			addr: "1.1.1.1",
			ok:   true,
		},
		{
			name: "invalid ipv4 address",
			addr: "1.1.A.1",
			ok:   false,
		},
		{
			name: "valid ipv6 subnet",
			addr: "2001:1::1",
			ok:   true,
		},
		{
			name: "invalid ipv6 address",
			addr: "2001:1:G::1",
			ok:   false,
		},
	}
	for _, tt := range tests {
		if err := ValidateIPAddress(tt.addr); err != nil {
			if tt.ok {
				t.Errorf("test: \"%s\" expected to succeed but failed with error: %+v", tt.name, err)
			}
		} else {
			if !tt.ok {
				t.Errorf("test: \"%s\" expected to fail but succeeded", tt.name)
			}
		}
	}
}

func TestValidationIPSubnet(t *testing.T) {
	tests := []struct {
		name   string
		subnet string
		ok     bool
	}{
		{
			name:   "valid ipv4 subnet",
			subnet: "1.1.1.1/24",
			ok:     true,
		},
		{
			name:   "invalid ipv4 subnet",
			subnet: "1.1.1.1/48",
			ok:     false,
		},
		{
			name:   "valid ipv6 subnet",
			subnet: "2001:1::1/64",
			ok:     true,
		},
		{
			name:   "invalid ipv6 subnet",
			subnet: "2001:1::1/132",
			ok:     false,
		},
	}

	for _, tt := range tests {
		if err := ValidateIPSubnet(tt.subnet); err != nil {
			if tt.ok {
				t.Errorf("test: \"%s\" expected to succeed but failed with error: %+v", tt.name, err)
			}
		} else {
			if !tt.ok {
				t.Errorf("test: \"%s\" expected to fail but succeeded", tt.name)
			}
		}
	}
}

func TestValidateRequestAuthentication(t *testing.T) {
	cases := []struct {
		name       string
		configName string
		in         proto.Message
		valid      bool
	}{
		{
			name:       "empty spec",
			configName: constants.DefaultAuthenticationPolicyName,
			in:         &security_beta.RequestAuthentication{},
			valid:      true,
		},
		{
			name:       "another empty spec",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				Selector: &api.WorkloadSelector{},
			},
			valid: true,
		},
		{
			name:       "empty spec with non default name",
			configName: someName,
			in:         &security_beta.RequestAuthentication{},
			valid:      true,
		},
		{
			name:       "default name with non empty selector",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
			},
			valid: true,
		},
		{
			name:       "empty jwt rule",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{},
				},
			},
			valid: false,
		},
		{
			name:       "empty issuer",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer: "",
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad JwksUri - no protocol",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "foo.com",
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad JwksUri - invalid port",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com:not-a-number",
					},
				},
			},
			valid: false,
		},
		{
			name:       "empy value",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app":     "httpbin",
						"version": "",
					},
				},
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com/cert",
					},
				},
			},
			valid: true,
		},
		{
			name:       "bad selector - empy key",
			configName: "foo",
			in: &security_beta.RequestAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
						"":    "v1",
					},
				},
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com/cert",
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad header location",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com",
						FromHeaders: []*security_beta.JWTHeader{
							{
								Name:   "",
								Prefix: "Bearer ",
							},
						},
					},
				},
			},
			valid: false,
		},
		{
			name:       "bad param location",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:     "foo.com",
						JwksUri:    "https://foo.com",
						FromParams: []string{""},
					},
				},
			},
			valid: false,
		},
		{
			name:       "good",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.RequestAuthentication{
				JwtRules: []*security_beta.JWTRule{
					{
						Issuer:  "foo.com",
						JwksUri: "https://foo.com",
						FromHeaders: []*security_beta.JWTHeader{
							{
								Name:   "x-foo",
								Prefix: "Bearer ",
							},
						},
					},
				},
			},
			valid: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateRequestAuthentication(c.configName, someNamespace, c.in); (got == nil) != c.valid {
				t.Errorf("got(%v) != want(%v)\n", got, c.valid)
			}
		})
	}
}

func TestValidatePeerAuthentication(t *testing.T) {
	cases := []struct {
		name       string
		configName string
		in         proto.Message
		valid      bool
	}{
		{
			name:       "empty spec",
			configName: constants.DefaultAuthenticationPolicyName,
			in:         &security_beta.PeerAuthentication{},
			valid:      true,
		},
		{
			name:       "empty mtls",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.PeerAuthentication{
				Selector: &api.WorkloadSelector{},
			},
			valid: true,
		},
		{
			name:       "empty spec with non default name",
			configName: someName,
			in:         &security_beta.PeerAuthentication{},
			valid:      true,
		},
		{
			name:       "default name with non empty selector",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.PeerAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
			},
			valid: true,
		},
		{
			name:       "empty port level mtls",
			configName: "foo",
			in: &security_beta.PeerAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				PortLevelMtls: map[uint32]*security_beta.PeerAuthentication_MutualTLS{},
			},
			valid: false,
		},
		{
			name:       "empty selector with port level mtls",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.PeerAuthentication{
				PortLevelMtls: map[uint32]*security_beta.PeerAuthentication_MutualTLS{
					8080: {
						Mode: security_beta.PeerAuthentication_MutualTLS_UNSET,
					},
				},
			},
			valid: false,
		},
		{
			name:       "port 0",
			configName: "foo",
			in: &security_beta.PeerAuthentication{
				PortLevelMtls: map[uint32]*security_beta.PeerAuthentication_MutualTLS{
					0: {
						Mode: security_beta.PeerAuthentication_MutualTLS_UNSET,
					},
				},
			},
			valid: false,
		},
		{
			name:       "unset mode",
			configName: constants.DefaultAuthenticationPolicyName,
			in: &security_beta.PeerAuthentication{
				Mtls: &security_beta.PeerAuthentication_MutualTLS{
					Mode: security_beta.PeerAuthentication_MutualTLS_UNSET,
				},
			},
			valid: true,
		},
		{
			name:       "port level",
			configName: "port-level",
			in: &security_beta.PeerAuthentication{
				Selector: &api.WorkloadSelector{
					MatchLabels: map[string]string{
						"app": "httpbin",
					},
				},
				PortLevelMtls: map[uint32]*security_beta.PeerAuthentication_MutualTLS{
					8080: {
						Mode: security_beta.PeerAuthentication_MutualTLS_UNSET,
					},
				},
			},
			valid: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidatePeerAuthentication(c.configName, someNamespace, c.in); (got == nil) != c.valid {
				t.Errorf("got(%v) != want(%v)\n", got, c.valid)
			}
		})
	}
}

func TestServiceSettings(t *testing.T) {
	cases := []struct {
		name  string
		hosts []string
		valid bool
	}{
		{
			name: "good",
			hosts: []string{
				"*.foo.bar",
				"bar.baz.svc.cluster.local",
			},
			valid: true,
		},
		{
			name: "bad",
			hosts: []string{
				"foo.bar.*",
			},
			valid: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := meshconfig.MeshConfig{
				ServiceSettings: []*meshconfig.MeshConfig_ServiceSettings{
					{
						Hosts: c.hosts,
					},
				},
			}

			if got := validateServiceSettings(&m); (got == nil) != c.valid {
				t.Errorf("got(%v) != want(%v)\n", got, c.valid)
			}
		})
	}
}

func TestValidateMeshNetworks(t *testing.T) {
	testcases := []struct {
		name  string
		mn    *meshconfig.MeshNetworks
		valid bool
	}{
		{
			name:  "Empty MeshNetworks",
			mn:    &meshconfig.MeshNetworks{},
			valid: true,
		},
		{
			name: "Valid MeshNetworks",
			mn: &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"n1": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{
									FromRegistry: "Kubernetes",
								},
							},
						},
						Gateways: []*meshconfig.Network_IstioNetworkGateway{
							{
								Gw: &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{
									RegistryServiceName: "istio-ingressgateway.istio-system.svc.cluster.local",
								},
								Port: 80,
							},
						},
					},
					"n2": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{
									FromRegistry: "cluster1",
								},
							},
						},
						Gateways: []*meshconfig.Network_IstioNetworkGateway{
							{
								Gw: &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{
									RegistryServiceName: "istio-ingressgateway.istio-system.svc.cluster.local",
								},
								Port: 443,
							},
						},
					},
				},
			},
			valid: true,
		},
		{
			name: "Invalid registry name",
			mn: &meshconfig.MeshNetworks{
				Networks: map[string]*meshconfig.Network{
					"n1": {
						Endpoints: []*meshconfig.Network_NetworkEndpoints{
							{
								Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{
									FromRegistry: "cluster.local",
								},
							},
						},
						Gateways: []*meshconfig.Network_IstioNetworkGateway{
							{
								Gw: &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{
									RegistryServiceName: "istio-ingressgateway.istio-system.svc.cluster.local",
								},
								Port: 80,
							},
						},
					},
				},
			},
			valid: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMeshNetworks(tc.mn)
			if err != nil && tc.valid {
				t.Errorf("error not expected on valid meshnetworks: %v", *tc.mn)
			}
			if err == nil && !tc.valid {
				t.Errorf("expected an error on invalid meshnetworks: %v", *tc.mn)
			}
		})
	}
}

func Test_validateExportTo(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		exportTo       []string
		isServiceEntry bool
		wantErr        bool
	}{
		{
			name:      "empty exportTo is okay",
			namespace: "ns5",
			wantErr:   false,
		},
		{
			name:      "* is allowed",
			namespace: "ns5",
			exportTo:  []string{"*"},
			wantErr:   false,
		},
		{
			name:      ". and ns1 are allowed",
			namespace: "ns5",
			exportTo:  []string{".", "ns1"},
			wantErr:   false,
		},
		{
			name:      "bunch of namespaces in exportTo is okay",
			namespace: "ns5",
			exportTo:  []string{"ns1", "ns2", "ns5"},
			wantErr:   false,
		},
		{
			name:           "~ is allowed for service entry configs",
			namespace:      "ns5",
			exportTo:       []string{"~"},
			isServiceEntry: true,
			wantErr:        false,
		},
		{
			name:      "~ not allowed for non service entry configs",
			namespace: "ns5",
			exportTo:  []string{"~", "ns1"},
			wantErr:   true,
		},
		{
			name:      ". and * together are not allowed",
			namespace: "ns5",
			exportTo:  []string{".", "*"},
			wantErr:   true,
		},
		{
			name:      "* and ns1 together are not allowed",
			namespace: "ns5",
			exportTo:  []string{"*", "ns1"},
			wantErr:   true,
		},
		{
			name:      ". and same namespace in exportTo is not okay",
			namespace: "ns5",
			exportTo:  []string{".", "ns5"},
			wantErr:   true,
		},
		{
			name:      "duplicate namespaces in exportTo is not okay",
			namespace: "ns5",
			exportTo:  []string{"ns1", "ns2", "ns1"},
			wantErr:   true,
		},
		{
			name:           "duplicate none in service entry exportTo is not okay",
			namespace:      "ns5",
			exportTo:       []string{"~", "~", "ns1"},
			isServiceEntry: true,
			wantErr:        true,
		},
		{
			name:      "invalid namespace names are not okay",
			namespace: "ns5",
			exportTo:  []string{"ns1_"},
			wantErr:   true,
		},
		{
			name:           "none and other namespaces cannot be combined in service entry exportTo",
			namespace:      "ns5",
			exportTo:       []string{"~", "ns1"},
			isServiceEntry: true,
			wantErr:        true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateExportTo(tt.namespace, tt.exportTo, tt.isServiceEntry); (err != nil) != tt.wantErr {
				t.Errorf("validateExportTo() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
