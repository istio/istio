// Copyright 2017 Istio Authors
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
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"

	authn "istio.io/api/authentication/v1alpha1"
	meshconfig "istio.io/api/mesh/v1alpha1"
	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
	networking "istio.io/api/networking/v1alpha3"
	rbac "istio.io/api/rbac/v1alpha1"
	"istio.io/istio/pilot/pkg/model/test"
)

const (
	// Config name for testing
	someName = "foo"
	// Config namespace for testing.
	someNamespace = "bar"
)

func TestConfigDescriptorValidate(t *testing.T) {
	badLabel := strings.Repeat("a", dns1123LabelMaxLength+1)
	goodLabel := strings.Repeat("a", dns1123LabelMaxLength-1)

	cases := []struct {
		name       string
		descriptor ConfigDescriptor
		wantErr    bool
	}{{
		name:       "Valid ConfigDescriptor (IstioConfig)",
		descriptor: IstioConfigTypes,
		wantErr:    false,
	}, {
		name: "Invalid DNS11234Label in ConfigDescriptor",
		descriptor: ConfigDescriptor{ProtoSchema{
			Type:        badLabel,
			MessageName: "istio.networking.v1alpha3.Gateway",
		}},
		wantErr: true,
	}, {
		name: "Bad MessageName in ProtoMessage",
		descriptor: ConfigDescriptor{ProtoSchema{
			Type:        goodLabel,
			MessageName: "nonexistent",
		}},
		wantErr: true,
	}, {
		name: "Missing key function",
		descriptor: ConfigDescriptor{ProtoSchema{
			Type:        "service-entry",
			MessageName: "istio.networking.v1alpha3.ServiceEtrny",
		}},
		wantErr: true,
	}, {
		name:       "Duplicate type and message",
		descriptor: ConfigDescriptor{DestinationRule, DestinationRule},
		wantErr:    true,
	}}

	for _, c := range cases {
		if err := c.descriptor.Validate(); (err != nil) != c.wantErr {
			t.Errorf("%v failed: got %v but wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

// ValidateConfig ensures that the config object is well-defined
// TODO: also check name and namespace
func descriptorValidateConfig(descriptor ConfigDescriptor, typ string, obj interface{}) error {
	if obj == nil {
		return fmt.Errorf("invalid nil configuration object")
	}

	t, ok := descriptor.GetByType(typ)
	if !ok {
		return fmt.Errorf("undeclared type: %q", typ)
	}

	v, ok := obj.(proto.Message)
	if !ok {
		return fmt.Errorf("cannot cast to a proto message")
	}

	if proto.MessageName(v) != t.MessageName {
		return fmt.Errorf("mismatched message type %q and type %q",
			proto.MessageName(v), t.MessageName)
	}
	return t.Validate(someName, someNamespace, v)
}

func TestConfigDescriptorValidateConfig(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		config  interface{}
		wantErr bool
	}{
		{
			name:    "bad configuration object",
			typ:     "policy",
			config:  nil,
			wantErr: true,
		},
		{
			name:    "undeclared kind",
			typ:     "special-type",
			config:  nil,
			wantErr: true,
		},
		{
			name:    "non-proto object configuration",
			typ:     "policy",
			config:  "non-proto objection configuration",
			wantErr: true,
		},
		{
			name:    "message type and kind mismatch",
			typ:     "policy",
			config:  ServiceEntry,
			wantErr: true,
		},
		{
			name:    "ProtoSchema validation1",
			typ:     "service-entry",
			config:  ServiceEntry,
			wantErr: true,
		},
		{
			name:    "Successful validation",
			typ:     MockConfig.Type,
			config:  &test.MockConfig{Key: "test"},
			wantErr: false,
		},
	}

	types := append(IstioConfigTypes, MockConfig)

	for _, c := range cases {
		if err := descriptorValidateConfig(types, c.typ, c.config); (err != nil) != c.wantErr {
			t.Errorf("%v failed: got error=%v but wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

var (
	endpoint1 = NetworkEndpoint{
		Address:     "192.168.1.1",
		Port:        10001,
		ServicePort: &Port{Name: "http", Port: 81, Protocol: ProtocolHTTP},
	}

	service1 = &Service{
		Hostname: "one.service.com",
		Address:  "192.168.3.1", // VIP
		Ports: PortList{
			&Port{Name: "http", Port: 81, Protocol: ProtocolHTTP},
			&Port{Name: "http-alt", Port: 8081, Protocol: ProtocolHTTP},
		},
	}
)

func TestServiceInstanceValidate(t *testing.T) {
	cases := []struct {
		name     string
		instance *ServiceInstance
		valid    bool
	}{
		{
			name: "nil service",
			instance: &ServiceInstance{
				Labels:   Labels{},
				Endpoint: endpoint1,
			},
		},
		{
			name: "bad label",
			instance: &ServiceInstance{
				Service:  service1,
				Labels:   Labels{"*": "-"},
				Endpoint: endpoint1,
			},
		},
		{
			name: "invalid service",
			instance: &ServiceInstance{
				Service: &Service{},
			},
		},
		{
			name: "invalid endpoint port and service port",
			instance: &ServiceInstance{
				Service: service1,
				Endpoint: NetworkEndpoint{
					Address: "192.168.1.2",
					Port:    -80,
				},
			},
		},
		{
			name: "endpoint missing service port",
			instance: &ServiceInstance{
				Service: service1,
				Endpoint: NetworkEndpoint{
					Address: "192.168.1.2",
					Port:    service1.Ports[1].Port,
					ServicePort: &Port{
						Name:     service1.Ports[1].Name + "-extra",
						Port:     service1.Ports[1].Port,
						Protocol: service1.Ports[1].Protocol,
					},
				},
			},
		},
		{
			name: "endpoint port and protocol mismatch",
			instance: &ServiceInstance{
				Service: service1,
				Endpoint: NetworkEndpoint{
					Address: "192.168.1.2",
					Port:    service1.Ports[1].Port,
					ServicePort: &Port{
						Name:     "http",
						Port:     service1.Ports[1].Port + 1,
						Protocol: ProtocolGRPC,
					},
				},
			},
		},
	}
	for _, c := range cases {
		t.Log("running case " + c.name)
		if got := c.instance.Validate(); (got == nil) != c.valid {
			t.Errorf("%s failed: got valid=%v but wanted valid=%v: %v", c.name, got == nil, c.valid, got)
		}
	}
}

func TestServiceValidate(t *testing.T) {
	ports := PortList{
		{Name: "http", Port: 80, Protocol: ProtocolHTTP},
		{Name: "http-alt", Port: 8080, Protocol: ProtocolHTTP},
	}
	badPorts := PortList{
		{Port: 80, Protocol: ProtocolHTTP},
		{Name: "http-alt^", Port: 8080, Protocol: ProtocolHTTP},
		{Name: "http", Port: -80, Protocol: ProtocolHTTP},
	}

	address := "192.168.1.1"

	cases := []struct {
		name    string
		service *Service
		valid   bool
	}{
		{
			name:    "empty hostname",
			service: &Service{Hostname: "", Address: address, Ports: ports},
		},
		{
			name:    "invalid hostname",
			service: &Service{Hostname: "hostname.^.com", Address: address, Ports: ports},
		},
		{
			name:    "empty ports",
			service: &Service{Hostname: "hostname", Address: address},
		},
		{
			name:    "bad ports",
			service: &Service{Hostname: "hostname", Address: address, Ports: badPorts},
		},
	}
	for _, c := range cases {
		if got := c.service.Validate(); (got == nil) != c.valid {
			t.Errorf("%s failed: got valid=%v but wanted valid=%v: %v", c.name, got == nil, c.valid, got)
		}
	}
}

func TestLabelsValidate(t *testing.T) {
	cases := []struct {
		name  string
		tags  Labels
		valid bool
	}{
		{
			name:  "empty tags",
			valid: true,
		},
		{
			name: "bad tag",
			tags: Labels{"^": "^"},
		},
		{
			name:  "good tag",
			tags:  Labels{"key": "value"},
			valid: true,
		},
		{
			name:  "good tag - empty value",
			tags:  Labels{"key": ""},
			valid: true,
		},
		{
			name: "bad tag - empty key",
			tags: Labels{"": "value"},
		},
		{
			name: "bad tag key 1",
			tags: Labels{".key": "value"},
		},
		{
			name: "bad tag key 2",
			tags: Labels{"key_": "value"},
		},
		{
			name: "bad tag key 3",
			tags: Labels{"key$": "value"},
		},
		{
			name: "bad tag value 1",
			tags: Labels{"key": ".value"},
		},
		{
			name: "bad tag value 2",
			tags: Labels{"key": "value_"},
		},
		{
			name: "bad tag value 3",
			tags: Labels{"key": "value$"},
		},
	}
	for _, c := range cases {
		if got := c.tags.Validate(); (got == nil) != c.valid {
			t.Errorf("%s failed: got valid=%v but wanted valid=%v: %v", c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateFQDN(t *testing.T) {
	if ValidateFQDN(strings.Repeat("x", 256)) == nil {
		t.Error("expected error on long FQDN")
	}
	if ValidateFQDN("") == nil {
		t.Error("expected error on empty FQDN")
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
		ConfigPath:                 "/etc/istio/proxy",
		BinaryPath:                 "/usr/local/bin/envoy",
		DiscoveryAddress:           "istio-pilot.istio-system:15010",
		ProxyAdminPort:             15000,
		DrainDuration:              types.DurationProto(45 * time.Second),
		ParentShutdownDuration:     types.DurationProto(60 * time.Second),
		ConnectTimeout:             types.DurationProto(10 * time.Second),
		ServiceCluster:             "istio-proxy",
		StatsdUdpAddress:           "istio-statsd-prom-bridge.istio-system:9125",
		EnvoyMetricsServiceAddress: "metrics-service.istio-system:15000",
		ControlPlaneAuthPolicy:     1,
		Tracing:                    nil,
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
			name:    "connect timeout invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.ConnectTimeout = types.DurationProto(-1 * time.Second) }),
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
			name:    "envoy metrics service address invalid",
			in:      modify(valid, func(c *meshconfig.ProxyConfig) { c.EnvoyMetricsServiceAddress = "metrics-service.istio-system" }),
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
								Secure:      false,
								CacertPath:  "/etc/lightstep/cacert.pem",
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
								Secure:      false,
								CacertPath:  "/etc/lightstep/cacert.pem",
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
								Secure:      false,
								CacertPath:  "/etc/lightstep/cacert.pem",
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
								Secure:      false,
								CacertPath:  "/etc/lightstep/cacert.pem",
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
								Secure:      false,
								CacertPath:  "/etc/lightstep/cacert.pem",
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
								Secure:      false,
								CacertPath:  "/etc/lightstep/cacert.pem",
							},
						},
					}
				},
			),
			isValid: false,
		},
		{
			name: "lightstep cacert is missing",
			in: modify(valid,
				func(c *meshconfig.ProxyConfig) {
					c.Tracing = &meshconfig.Tracing{
						Tracer: &meshconfig.Tracing_Lightstep_{
							Lightstep: &meshconfig.Tracing_Lightstep{
								Address:     "collector.lightstep:8080",
								AccessToken: "abcdefg1234567",
								Secure:      true,
								CacertPath:  "",
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
		ConfigPath:                 "",
		BinaryPath:                 "",
		DiscoveryAddress:           "10.0.0.100",
		ProxyAdminPort:             0,
		DrainDuration:              types.DurationProto(-1 * time.Second),
		ParentShutdownDuration:     types.DurationProto(-1 * time.Second),
		ConnectTimeout:             types.DurationProto(-1 * time.Second),
		ServiceCluster:             "",
		StatsdUdpAddress:           "10.0.0.100",
		EnvoyMetricsServiceAddress: "metrics-service",
		ControlPlaneAuthPolicy:     -1,
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
				Tls:   &networking.Server_TLSOptions{Mode: networking.Server_TLSOptions_SIMPLE},
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
				Tls:   &networking.Server_TLSOptions{Mode: networking.Server_TLSOptions_SIMPLE},
			},
			"cannot have TLS"},
		{"tls redirect on HTTP",
			&networking.Server{
				Hosts: []string{"foo.bar.com"},
				Port:  &networking.Port{Number: 10000, Name: "http", Protocol: "http"},
				Tls: &networking.Server_TLSOptions{
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
		in   *networking.Server_TLSOptions
		out  string
	}{
		{"empty", &networking.Server_TLSOptions{}, ""},
		{"simple",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh"},
			""},
		{"simple with client bundle",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker"},
			""},
		{"simple sds with client bundle",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
				CredentialName:    "sds-name"},
			""},
		{"simple no server cert",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_SIMPLE,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
			},
			"server certificate"},
		{"simple no private key",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        ""},
			"private key"},
		{"simple sds no server cert",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_SIMPLE,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
				CredentialName:    "sds-name",
			},
			""},
		{"simple sds no private key",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "",
				CredentialName:    "sds-name",
			},
			""},
		{"mutual",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker"},
			""},
		{"mutual sds",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
				CredentialName:    "sds-name",
			},
			""},
		{"mutual no server cert",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker"},
			"server certificate"},
		{"mutual sds no server cert",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    "Commander William T. Riker",
				CredentialName:    "sds-name"},
			""},
		{"mutual no client CA bundle",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				PrivateKey:        "Khan Noonien Singh",
				CaCertificates:    ""},
			"client CA bundle"},
		// this pair asserts we get errors about both client and server certs missing when in mutual mode
		// and both are absent, but requires less rewriting of the testing harness than merging the cases
		{"mutual no certs",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    ""},
			"server certificate"},
		{"mutual no certs",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    ""},
			"private key"},
		{"mutual no certs",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "",
				PrivateKey:        "",
				CaCertificates:    ""},
			"client CA bundle"},
		{"pass through sds no certs",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_PASSTHROUGH,
				ServerCertificate: "",
				CaCertificates:    "",
				CredentialName:    "sds-name"},
			""},
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
		{0, true},
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
			Percent: 20,
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: true},
		{name: "valid default", in: &networking.HTTPFaultInjection_Abort{
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: true},
		{name: "invalid percent", in: &networking.HTTPFaultInjection_Abort{
			Percent: -1,
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 200,
			},
		}, valid: false},
		{name: "invalid http status", in: &networking.HTTPFaultInjection_Abort{
			Percent: 20,
			ErrorType: &networking.HTTPFaultInjection_Abort_HttpStatus{
				HttpStatus: 9000,
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
			Percent: 20,
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
			Percent: 101,
			HttpDelayType: &networking.HTTPFaultInjection_Delay_FixedDelay{
				FixedDelay: &types.Duration{Seconds: 3},
			},
		}, valid: false},
		{name: "invalid delay", in: &networking.HTTPFaultInjection_Delay{
			Percent: 20,
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
			if err := validatePortName(tc.name); (err == nil) != tc.valid {
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
			name: "normal redirect",
			redirect: &networking.HTTPRedirect{
				Uri:       "t",
				Authority: "t",
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
					Port: &networking.PortSelector_Number{
						Number: 5000,
					},
				},
			},
			valid: true,
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
			AppendRequestHeaders: map[string]string{
				"name": "",
			},
			AppendResponseHeaders: map[string]string{
				"name": "",
			},
		}, valid: true},
		{name: "empty request response headers", route: &networking.HTTPRoute{
			Route: []*networking.HTTPRouteDestination{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
			AppendRequestHeaders: map[string]string{
				"": "value",
			},
			AppendResponseHeaders: map[string]string{
				"": "value",
			},
		}, valid: false},
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateHTTPRoute(tc.route); (err == nil) != tc.valid {
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
		}}, valid: true},
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
		{name: "valid removeResponseHeaders", in: &networking.VirtualService{
			Hosts: []string{"foo.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.HTTPRouteDestination{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
				RemoveResponseHeaders: []string{"unwantedHeader", "secretStuff"},
			}},
		}, valid: true},
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
					ConsecutiveErrors: 5,
					MinHealthPercent:  20,
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
					ConsecutiveErrors: 5,
					MinHealthPercent:  20,
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
							ConsecutiveErrors: 5,
							MinHealthPercent:  20,
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
							ConsecutiveErrors: 5,
							MinHealthPercent:  20,
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
					ConsecutiveErrors: 5,
					MinHealthPercent:  20,
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
							ConsecutiveErrors: 5,
							MinHealthPercent:  30,
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
				ConsecutiveErrors: 5,
				MinHealthPercent:  20,
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
						ConsecutiveErrors: 5,
						MinHealthPercent:  20,
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
				ConsecutiveErrors: 5,
				MinHealthPercent:  20,
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
				ConsecutiveErrors: 5,
				MinHealthPercent:  -1,
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
	duration := time.Hour
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
			ConsecutiveErrors:  5,
			Interval:           &types.Duration{Seconds: 2},
			BaseEjectionTime:   &types.Duration{Seconds: 2},
			MaxEjectionPercent: 50,
		}, valid: true},

		{name: "invalid outlier detection, bad consecutive errors", in: networking.OutlierDetection{
			ConsecutiveErrors: -1},
			valid: false},

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
		{name: "empty filters", in: &networking.EnvoyFilter{}, error: "missing filters"},

		{name: "missing relativeTo", in: &networking.EnvoyFilter{
			Filters: []*networking.EnvoyFilter_Filter{
				{
					InsertPosition: &networking.EnvoyFilter_InsertPosition{
						Index: networking.EnvoyFilter_InsertPosition_AFTER,
					},
					FilterType:   networking.EnvoyFilter_Filter_NETWORK,
					FilterName:   "envoy.foo",
					FilterConfig: &types.Struct{},
				},
			},
		}, error: "missing relativeTo"},

		{name: "missing filter type", in: &networking.EnvoyFilter{
			Filters: []*networking.EnvoyFilter_Filter{
				{
					InsertPosition: &networking.EnvoyFilter_InsertPosition{
						Index: networking.EnvoyFilter_InsertPosition_FIRST,
					},
					FilterName:   "envoy.foo",
					FilterConfig: &types.Struct{},
				},
			},
		}, error: "missing filter type"},

		{name: "missing filter name", in: &networking.EnvoyFilter{
			Filters: []*networking.EnvoyFilter_Filter{
				{
					InsertPosition: &networking.EnvoyFilter_InsertPosition{
						Index: networking.EnvoyFilter_InsertPosition_FIRST,
					},
					FilterType:   networking.EnvoyFilter_Filter_NETWORK,
					FilterConfig: &types.Struct{},
				},
			},
		}, error: "missing filter name"},

		{name: "missing filter config", in: &networking.EnvoyFilter{
			Filters: []*networking.EnvoyFilter_Filter{
				{
					InsertPosition: &networking.EnvoyFilter_InsertPosition{
						Index: networking.EnvoyFilter_InsertPosition_FIRST,
					},
					FilterType: networking.EnvoyFilter_Filter_NETWORK,
					FilterName: "envoy.foo",
				},
			},
		}, error: "missing filter config"},

		{name: "happy filter config", in: &networking.EnvoyFilter{
			Filters: []*networking.EnvoyFilter_Filter{
				{
					InsertPosition: &networking.EnvoyFilter_InsertPosition{
						Index: networking.EnvoyFilter_InsertPosition_FIRST,
					},
					FilterType:   networking.EnvoyFilter_Filter_NETWORK,
					FilterName:   "envoy.foo",
					FilterConfig: &types.Struct{},
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
				{Address: "lon.google.com", Ports: map[string]uint32{"http-valid1": 8080}},
				{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			valid: false},

		{name: "discovery type static, bad endpoint port name", in: networking.ServiceEntry{
			Hosts:     []string{"google.com"},
			Addresses: []string{"172.1.2.16"},
			Ports: []*networking.Port{
				{Number: 80, Protocol: "http", Name: "http-valid1"},
				{Number: 8080, Protocol: "http", Name: "http-valid2"},
			},
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
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
			Endpoints: []*networking.ServiceEntry_Endpoint{
				{Address: "unix:///path/to/socket"},
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

func TestValidateAuthenticationPolicy(t *testing.T) {
	cases := []struct {
		name       string
		configName string
		in         proto.Message
		valid      bool
	}{
		{
			name:       "empty policy with namespace-wide policy name",
			configName: DefaultAuthenticationPolicyName,
			in:         &authn.Policy{},
			valid:      true,
		},
		{
			name:       "empty policy with non-default name",
			configName: someName,
			in:         &authn.Policy{},
			valid:      false,
		},
		{
			name:       "service-specific policy with namespace-wide name",
			configName: DefaultAuthenticationPolicyName,
			in: &authn.Policy{
				Targets: []*authn.TargetSelector{{
					Name: "foo",
				}},
			},
			valid: false,
		},
		{
			name:       "Targets only policy",
			configName: someName,
			in: &authn.Policy{
				Targets: []*authn.TargetSelector{{
					Name: "foo",
				}},
			},
			valid: true,
		},
		{
			name:       "Source mTLS",
			configName: DefaultAuthenticationPolicyName,
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			valid: true,
		},
		{
			name:       "Source JWT",
			configName: DefaultAuthenticationPolicyName,
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Jwt{
						Jwt: &authn.Jwt{
							Issuer:     "istio.io",
							JwksUri:    "https://secure.istio.io/oauth/v1/certs",
							JwtHeaders: []string{"x-goog-iap-jwt-assertion"},
						},
					},
				}},
			},
			valid: true,
		},
		{
			name:       "Origin",
			configName: DefaultAuthenticationPolicyName,
			in: &authn.Policy{
				Origins: []*authn.OriginAuthenticationMethod{
					{
						Jwt: &authn.Jwt{
							Issuer:     "istio.io",
							JwksUri:    "https://secure.istio.io/oauth/v1/certs",
							JwtHeaders: []string{"x-goog-iap-jwt-assertion"},
						},
					},
				},
			},
			valid: true,
		},
		{
			name:       "Bad JkwsURI",
			configName: DefaultAuthenticationPolicyName,
			in: &authn.Policy{
				Origins: []*authn.OriginAuthenticationMethod{
					{
						Jwt: &authn.Jwt{
							Issuer:     "istio.io",
							JwksUri:    "secure.istio.io/oauth/v1/certs",
							JwtHeaders: []string{"x-goog-iap-jwt-assertion"},
						},
					},
				},
			},
			valid: false,
		},
		{
			name:       "Bad JkwsURI Port",
			configName: DefaultAuthenticationPolicyName,
			in: &authn.Policy{
				Origins: []*authn.OriginAuthenticationMethod{
					{
						Jwt: &authn.Jwt{
							Issuer:     "istio.io",
							JwksUri:    "https://secure.istio.io:not-a-number/oauth/v1/certs",
							JwtHeaders: []string{"x-goog-iap-jwt-assertion"},
						},
					},
				},
			},
			valid: false,
		},
		{
			name:       "Duplicate Jwt issuers",
			configName: DefaultAuthenticationPolicyName,
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Jwt{
						Jwt: &authn.Jwt{
							Issuer:     "istio.io",
							JwksUri:    "https://secure.istio.io/oauth/v1/certs",
							JwtHeaders: []string{"x-goog-iap-jwt-assertion"},
						},
					},
				}},
				Origins: []*authn.OriginAuthenticationMethod{
					{
						Jwt: &authn.Jwt{
							Issuer:     "istio.io",
							JwksUri:    "https://secure.istio.io/oauth/v1/certs",
							JwtHeaders: []string{"x-goog-iap-jwt-assertion"},
						},
					},
				},
			},
			valid: false,
		},
		{
			name:       "Just binding",
			configName: DefaultAuthenticationPolicyName,
			in: &authn.Policy{
				PrincipalBinding: authn.PrincipalBinding_USE_ORIGIN,
			},
			valid: true,
		},
		{
			name:       "Bad target name",
			configName: someName,
			in: &authn.Policy{
				Targets: []*authn.TargetSelector{
					{
						Name: "foo.bar",
					},
				},
			},
			valid: false,
		},
		{
			name:       "Good target name",
			configName: someName,
			in: &authn.Policy{
				Targets: []*authn.TargetSelector{
					{
						Name: "good-service-name",
					},
				},
			},
			valid: true,
		},
	}
	for _, c := range cases {
		if got := ValidateAuthenticationPolicy(c.configName, someNamespace, c.in); (got == nil) != c.valid {
			t.Errorf("ValidateAuthenticationPolicy(%v): got(%v) != want(%v): %v\n", c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateAuthenticationMeshPolicy(t *testing.T) {
	cases := []struct {
		name       string
		configName string
		in         proto.Message
		valid      bool
	}{
		{
			name:       "good name",
			configName: DefaultAuthenticationPolicyName,
			in:         &authn.Policy{},
			valid:      true,
		},
		{
			name:       "bad-name",
			configName: someName,
			in:         &authn.Policy{},
			valid:      false,
		},
		{
			name:       "has targets",
			configName: DefaultAuthenticationPolicyName,
			in: &authn.Policy{
				Targets: []*authn.TargetSelector{{
					Name: "foo",
				}},
			},
			valid: false,
		},
		{
			name:       "good",
			configName: DefaultAuthenticationPolicyName,
			in: &authn.Policy{
				Peers: []*authn.PeerAuthenticationMethod{{
					Params: &authn.PeerAuthenticationMethod_Mtls{},
				}},
			},
			valid: true,
		},
	}
	for _, c := range cases {
		if got := ValidateAuthenticationPolicy(c.configName, "", c.in); (got == nil) != c.valid {
			t.Errorf("ValidateAuthenticationPolicy(%v): got(%v) != want(%v): %v\n", c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateServiceRole(t *testing.T) {
	cases := []struct {
		name         string
		in           proto.Message
		expectErrMsg string
	}{
		{
			name:         "invalid proto",
			expectErrMsg: "cannot cast to ServiceRole",
		},
		{
			name:         "empty rules",
			in:           &rbac.ServiceRole{},
			expectErrMsg: "at least 1 rule must be specified",
		},
		{
			name: "has both methods and not_methods",
			in: &rbac.ServiceRole{Rules: []*rbac.AccessRule{
				{
					Methods:    []string{"GET", "POST"},
					NotMethods: []string{"DELETE"},
				},
			}},
			expectErrMsg: "cannot have both regular and *not* attributes for the same kind (i.e. methods and not_methods) for rule 0",
		},
		{
			name: "has both ports and not_ports",
			in: &rbac.ServiceRole{Rules: []*rbac.AccessRule{
				{
					Ports:    []int32{9080},
					NotPorts: []int32{443},
				},
			}},
			expectErrMsg: "cannot have both regular and *not* attributes for the same kind (i.e. ports and not_ports) for rule 0",
		},
		{
			name: "has out of range port",
			in: &rbac.ServiceRole{Rules: []*rbac.AccessRule{
				{
					Ports: []int32{9080, -80},
				},
			}},
			expectErrMsg: "at least one port is not in the range of [0, 65535]",
		},
		{
			name: "has both first-class field and constraints",
			in: &rbac.ServiceRole{Rules: []*rbac.AccessRule{
				{
					Ports: []int32{9080},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "destination.port", Values: []string{"80"}},
					},
				},
			}},
			expectErrMsg: "cannot define destination.port for rule 0 because a similar first-class field has been defined",
		},
		{
			name: "no key in constraint",
			in: &rbac.ServiceRole{Rules: []*rbac.AccessRule{
				{
					Methods: []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
				{
					Methods: []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Values: []string{"value"}},
					},
				},
			}},
			expectErrMsg: "key cannot be empty for constraint 1 in rule 1",
		},
		{
			name: "no value in constraint",
			in: &rbac.ServiceRole{Rules: []*rbac.AccessRule{
				{
					Methods: []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
				{
					Methods: []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{}},
					},
				},
			}},
			expectErrMsg: "at least 1 value must be specified for constraint 1 in rule 1",
		},
		{
			name: "success proto",
			in: &rbac.ServiceRole{Rules: []*rbac.AccessRule{
				{
					Methods:  []string{"GET", "POST"},
					NotHosts: []string{"finances.google.com"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
				{
					Methods: []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
			}},
		},
	}
	for _, c := range cases {
		err := ValidateServiceRole(someName, someNamespace, c.in)
		if err == nil {
			if len(c.expectErrMsg) != 0 {
				t.Errorf("ValidateServiceRole(%v): got nil but want %q\n", c.name, c.expectErrMsg)
			}
		} else if err.Error() != c.expectErrMsg {
			t.Errorf("ValidateServiceRole(%v): got %q but want %q\n", c.name, err.Error(), c.expectErrMsg)
		}
	}
}

func TestValidateServiceRoleBinding(t *testing.T) {
	cases := []struct {
		name         string
		in           proto.Message
		expectErrMsg string
	}{
		{
			name:         "invalid proto",
			expectErrMsg: "cannot cast to ServiceRoleBinding",
		},
		{
			name: "no subject",
			in: &rbac.ServiceRoleBinding{
				Subjects: []*rbac.Subject{},
				RoleRef:  &rbac.RoleRef{Kind: "ServiceRole", Name: "ServiceRole001"},
			},
			expectErrMsg: "at least 1 subject must be specified",
		},
		{
			name: "no user, group and properties",
			in: &rbac.ServiceRoleBinding{
				Subjects: []*rbac.Subject{
					{User: "User0", Group: "Group0", Properties: map[string]string{"prop0": "value0"}},
					{User: "", Group: "", Properties: map[string]string{}},
				},
				RoleRef: &rbac.RoleRef{Kind: "ServiceRole", Name: "ServiceRole001"},
			},
			expectErrMsg: "empty subjects are not allowed. Found an empty subject at index 1",
		},
		{
			name: "no roleRef",
			in: &rbac.ServiceRoleBinding{
				Subjects: []*rbac.Subject{
					{User: "User0", Group: "Group0", Properties: map[string]string{"prop0": "value0"}},
					{User: "User1", Group: "Group1", Properties: map[string]string{"prop1": "value1"}},
				},
			},
			expectErrMsg: "exactly one of `roleRef`, `role`, or `actions` must be specified",
		},
		{
			name: "incorrect kind",
			in: &rbac.ServiceRoleBinding{
				Subjects: []*rbac.Subject{
					{User: "User0", Group: "Group0", Properties: map[string]string{"prop0": "value0"}},
					{User: "User1", Group: "Group1", Properties: map[string]string{"prop1": "value1"}},
				},
				RoleRef: &rbac.RoleRef{Kind: "ServiceRoleTypo", Name: "ServiceRole001"},
			},
			expectErrMsg: `kind set to "ServiceRoleTypo", currently the only supported value is "ServiceRole"`,
		},
		{
			name: "no name",
			in: &rbac.ServiceRoleBinding{
				Subjects: []*rbac.Subject{
					{User: "User0", Group: "Group0", Properties: map[string]string{"prop0": "value0"}},
					{User: "User1", Group: "Group1", Properties: map[string]string{"prop1": "value1"}},
				},
				Role: "/",
			},
			expectErrMsg: "`role` cannot have an empty ServiceRole name",
		},
		{
			name: "no name",
			in: &rbac.ServiceRoleBinding{
				Subjects: []*rbac.Subject{
					{User: "User0", Group: "Group0", Properties: map[string]string{"prop0": "value0"}},
					{User: "User1", Group: "Group1", Properties: map[string]string{"prop1": "value1"}},
				},
				RoleRef: &rbac.RoleRef{Kind: "ServiceRole", Name: ""},
			},
			expectErrMsg: "`name` in `roleRef` cannot be empty",
		},
		{
			name: "first-class field already exists",
			in: &rbac.ServiceRoleBinding{
				Subjects: []*rbac.Subject{
					{Namespaces: []string{"default"}, Properties: map[string]string{"source.namespace": "istio-system"}},
				},
				RoleRef: &rbac.RoleRef{Kind: "ServiceRole", Name: "ServiceRole001"},
			},
			expectErrMsg: "cannot define source.namespace for binding 0 because a similar first-class field has been defined",
		},
		{
			name: "use * for names",
			in: &rbac.ServiceRoleBinding{
				Subjects: []*rbac.Subject{
					{Names: []string{"*"}},
				},
				RoleRef: &rbac.RoleRef{Kind: "ServiceRole", Name: "ServiceRole001"},
			},
			expectErrMsg: "do not use * for names or not_names (in rule 0)",
		},
		{
			name: "success proto",
			in: &rbac.ServiceRoleBinding{
				Subjects: []*rbac.Subject{
					{User: "User0", Group: "Group0", Properties: map[string]string{"prop0": "value0"}},
					{User: "User1", Group: "Group1", Properties: map[string]string{"prop1": "value1"}},
				},
				RoleRef: &rbac.RoleRef{Kind: "ServiceRole", Name: "ServiceRole001"},
			},
		},
	}
	for _, c := range cases {
		err := ValidateServiceRoleBinding(someName, someNamespace, c.in)
		if err == nil {
			if len(c.expectErrMsg) != 0 {
				t.Errorf("ValidateServiceRoleBinding(%v): got nil but want %q\n", c.name, c.expectErrMsg)
			}
		} else if err.Error() != c.expectErrMsg {
			t.Errorf("ValidateServiceRoleBinding(%v): got %q but want %q\n", c.name, err.Error(), c.expectErrMsg)
		}
	}
}

func TestValidateAuthorizationPolicy(t *testing.T) {
	cases := []struct {
		name         string
		in           proto.Message
		expectErrMsg string
	}{
		{
			name:         "invalid proto",
			expectErrMsg: "cannot cast to AuthorizationPolicy",
		},
		{
			name: "proto with no roleRef or inline role definition",
			in: &rbac.AuthorizationPolicy{
				Allow: []*rbac.ServiceRoleBinding{
					{
						Subjects: []*rbac.Subject{
							{
								Namespaces: []string{"default, istio-system"},
							},
						},
					},
				},
			},
			expectErrMsg: "exactly one of `roleRef`, `role`, or `actions` must be specified",
		},
		{
			name: "proto with both roleRef and inline role definition",
			in: &rbac.AuthorizationPolicy{
				Allow: []*rbac.ServiceRoleBinding{
					{
						Subjects: []*rbac.Subject{
							{
								Namespaces: []string{"default, istio-system"},
							},
						},
						RoleRef: &rbac.RoleRef{
							Kind: "ServiceRole",
							Name: "service-role-1",
						},
						Actions: []*rbac.AccessRule{
							{
								Ports: []int32{3000},
							},
						},
					},
				},
			},
			expectErrMsg: "exactly one of `roleRef`, `role`, or `actions` must be specified",
		},
		{
			name: "proto with both roleRef and role",
			in: &rbac.AuthorizationPolicy{
				Allow: []*rbac.ServiceRoleBinding{
					{
						Subjects: []*rbac.Subject{
							{
								Namespaces: []string{"default, istio-system"},
							},
						},
						RoleRef: &rbac.RoleRef{
							Kind: "ServiceRole",
							Name: "service-role-1",
						},
						Role: "service-role-1",
					},
				},
			},
			expectErrMsg: "exactly one of `roleRef`, `role`, or `actions` must be specified",
		},
		{
			name: "proto with both role and inline role definition",
			in: &rbac.AuthorizationPolicy{
				Allow: []*rbac.ServiceRoleBinding{
					{
						Subjects: []*rbac.Subject{
							{
								Namespaces: []string{"default, istio-system"},
							},
						},
						Role: "service-role-1",
						Actions: []*rbac.AccessRule{
							{
								Ports: []int32{3000},
							},
						},
					},
				},
			},
			expectErrMsg: "exactly one of `roleRef`, `role`, or `actions` must be specified",
		},
		{
			name: "success proto with roleRef",
			in: &rbac.AuthorizationPolicy{
				Allow: []*rbac.ServiceRoleBinding{
					{
						Subjects: []*rbac.Subject{
							{
								Namespaces: []string{"default, istio-system"},
							},
						},
						RoleRef: &rbac.RoleRef{
							Kind: "ServiceRole",
							Name: "service-role-1",
						},
					},
				},
			},
		},
		{
			name: "proto with inline but invalid role definition",
			in: &rbac.AuthorizationPolicy{
				Allow: []*rbac.ServiceRoleBinding{
					{
						Subjects: []*rbac.Subject{
							{
								Namespaces: []string{"default, istio-system"},
							},
						},
						Actions: []*rbac.AccessRule{
							{
								Ports:    []int32{3000},
								NotPorts: []int32{8080},
							},
						},
					},
				},
			},
			expectErrMsg: "cannot have both regular and *not* attributes for the same kind (i.e. ports and not_ports) for rule 0",
		},
		{
			name: "success proto with inline role definition",
			in: &rbac.AuthorizationPolicy{
				Allow: []*rbac.ServiceRoleBinding{
					{
						Subjects: []*rbac.Subject{
							{
								Namespaces: []string{"default, istio-system"},
							},
						},
						Actions: []*rbac.AccessRule{
							{
								Methods: []string{"GET"},
							},
						},
					},
				},
			},
		},
	}
	for _, c := range cases {
		err := ValidateAuthorizationPolicy(someName, someNamespace, c.in)
		if err == nil {
			if len(c.expectErrMsg) != 0 {
				t.Errorf("ValidateAuthorizationPolicy(%v): got nil but want %q\n", c.name, c.expectErrMsg)
			}
		} else if err.Error() != c.expectErrMsg {
			t.Errorf("ValidateAuthorizationPolicy(%v): got %q but want %q\n", c.name, err.Error(), c.expectErrMsg)
		}
	}
}

func TestValidateNetworkEndpointAddress(t *testing.T) {
	testCases := []struct {
		name  string
		ne    *NetworkEndpoint
		valid bool
	}{
		{
			"Unix OK",
			&NetworkEndpoint{Family: AddressFamilyUnix, Address: "/absolute/path"},
			true,
		},
		{
			"IP OK",
			&NetworkEndpoint{Address: "12.3.4.5", Port: 76},
			true,
		},
		{
			"Unix not absolute",
			&NetworkEndpoint{Family: AddressFamilyUnix, Address: "./socket"},
			false,
		},
		{
			"IP invalid",
			&NetworkEndpoint{Address: "260.3.4.5", Port: 76},
			false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNetworkEndpointAddress(tc.ne)
			if tc.valid && err != nil {
				t.Fatalf("ValidateAddress() => want error nil got %v", err)
			} else if !tc.valid && err == nil {
				t.Fatalf("ValidateAddress() => want error got nil")
			}
		})
	}
}

func TestValidateClusterRbacConfig(t *testing.T) {
	cases := []struct {
		caseName     string
		name         string
		namespace    string
		in           proto.Message
		expectErrMsg string
	}{
		{
			caseName:     "invalid proto",
			expectErrMsg: "cannot cast to ClusterRbacConfig",
		},
		{
			caseName: "invalid name",
			name:     "cluster-rbac-config",
			in:       &rbac.RbacConfig{Mode: rbac.RbacConfig_ON_WITH_INCLUSION},
			expectErrMsg: fmt.Sprintf("ClusterRbacConfig has invalid name(cluster-rbac-config), name must be %q",
				DefaultRbacConfigName),
		},
		{
			caseName: "success proto",
			name:     DefaultRbacConfigName,
			in:       &rbac.RbacConfig{Mode: rbac.RbacConfig_ON},
		},
		{
			caseName:     "empty exclusion",
			name:         DefaultRbacConfigName,
			in:           &rbac.RbacConfig{Mode: rbac.RbacConfig_ON_WITH_EXCLUSION},
			expectErrMsg: "exclusion cannot be null (use 'exclusion: {}' for none)",
		},
		{
			caseName:     "empty inclusion",
			name:         DefaultRbacConfigName,
			in:           &rbac.RbacConfig{Mode: rbac.RbacConfig_ON_WITH_INCLUSION},
			expectErrMsg: "inclusion cannot be null (use 'inclusion: {}' for none)",
		},
	}
	for _, c := range cases {
		err := ValidateClusterRbacConfig(c.name, c.namespace, c.in)
		if err == nil {
			if len(c.expectErrMsg) != 0 {
				t.Errorf("ValidateClusterRbacConfig(%v): got nil but want %q\n", c.caseName, c.expectErrMsg)
			}
		} else if err.Error() != c.expectErrMsg {
			t.Errorf("ValidateClusterRbacConfig(%v): got %q but want %q\n", c.caseName, err.Error(), c.expectErrMsg)
		}
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
			name: "invalid domian or labels",
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
		{"UDS bind", &networking.Sidecar{
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
		{"UDS bind 2", &networking.Sidecar{
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
		in    *meshconfig.LocalityLoadBalancerSetting
		valid bool
	}{
		{
			name:  "valid mesh config without LocalityLoadBalancerSetting",
			in:    nil,
			valid: true,
		},

		{
			name: "invalid LocalityLoadBalancerSetting_Distribute total weight > 100",
			in: &meshconfig.LocalityLoadBalancerSetting{
				Distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
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
			in: &meshconfig.LocalityLoadBalancerSetting{
				Distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
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
			in: &meshconfig.LocalityLoadBalancerSetting{
				Distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
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
			in: &meshconfig.LocalityLoadBalancerSetting{
				Distribute: []*meshconfig.LocalityLoadBalancerSetting_Distribute{
					{
						From: "a/b/c",
						To: map[string]uint32{
							"a/b/c": 80,
							"a/b1":  20,
						},
					},
				},
				Failover: []*meshconfig.LocalityLoadBalancerSetting_Failover{
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
			in: &meshconfig.LocalityLoadBalancerSetting{
				Failover: []*meshconfig.LocalityLoadBalancerSetting_Failover{
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
