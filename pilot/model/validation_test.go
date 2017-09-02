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
	"strings"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/duration"
	multierror "github.com/hashicorp/go-multierror"

	proxyconfig "istio.io/api/proxy/v1/config"
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
			MessageName: RouteRule.MessageName,
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
			Type:        RouteRule.Type,
			MessageName: RouteRule.MessageName,
		}},
		wantErr: true,
	}, {
		name:       "Duplicate type and message",
		descriptor: ConfigDescriptor{RouteRule, RouteRule},
		wantErr:    true,
	}}

	for _, c := range cases {
		if err := c.descriptor.Validate(); (err != nil) != c.wantErr {
			t.Errorf("%v failed: got %v but wantErr=%v", c.name, err, c.wantErr)
		}
	}
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
			typ:     RouteRule.Type,
			config:  nil,
			wantErr: true,
		},
		{
			name:    "undeclared kind",
			typ:     "special-type",
			config:  &proxyconfig.RouteRule{},
			wantErr: true,
		},
		{
			name:    "non-proto object configuration",
			typ:     RouteRule.Type,
			config:  "non-proto objection configuration",
			wantErr: true,
		},
		{
			name: "message type and kind mismatch",
			typ:  RouteRule.Type,
			config: &proxyconfig.DestinationPolicy{
				Destination: "foo",
			},
			wantErr: true,
		},
		{
			name:    "ProtoSchema validation1",
			typ:     RouteRule.Type,
			config:  &proxyconfig.RouteRule{},
			wantErr: true,
		},
		{
			name: "ProtoSchema validation2",
			typ:  RouteRule.Type,
			config: &proxyconfig.RouteRule{
				Destination: "foo",
				Name:        "test",
			},
			wantErr: false,
		},
	}

	for _, c := range cases {
		if err := IstioConfigTypes.ValidateConfig(c.typ, c.config); (err != nil) != c.wantErr {
			t.Errorf("%v failed: got error=%v but wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func TestServiceInstanceValidate(t *testing.T) {
	cases := []struct {
		name     string
		instance *ServiceInstance
		valid    bool
	}{
		{
			name: "nil service",
			instance: &ServiceInstance{
				Tags:     Tags{},
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

func TestTagsValidate(t *testing.T) {
	cases := []struct {
		name  string
		tags  Tags
		valid bool
	}{
		{
			name:  "empty tags",
			valid: true,
		},
		{
			name: "bad tag",
			tags: Tags{"^": "^"},
		},
		{
			name:  "good tag",
			tags:  Tags{"key": "value"},
			valid: true,
		},
	}
	for _, c := range cases {
		if got := c.tags.Validate(); (got == nil) != c.valid {
			t.Errorf("%s failed: got valid=%v but wanted valid=%v: %v", c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateRouteAndIngressRule(t *testing.T) {
	cases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{name: "empty destination policy", in: &proxyconfig.DestinationPolicy{}, valid: false},
		{name: "empty route rule", in: &proxyconfig.RouteRule{}, valid: false},
		{name: "route rule w destination", in: &proxyconfig.RouteRule{Destination: "foobar", Name: "test"}, valid: true},
		{name: "route rule bad destination", in: &proxyconfig.RouteRule{
			Destination: "badhost@.default.svc.cluster.local",
			Name:        "test",
		},
			valid: false},
		{name: "route rule bad match source", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{Source: "somehost!.default.svc.cluster.local"},
		},
			valid: false},
		{name: "route rule bad weight dest", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{Source: "somehost.default.svc.cluster.local"},
			Route: []*proxyconfig.DestinationWeight{
				{Destination: strings.Repeat(strings.Repeat("1234567890", 6)+".", 6)},
			},
		},
			valid: false},
		{name: "route rule bad weight", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{Source: "somehost.default.svc.cluster.local"},
			Route: []*proxyconfig.DestinationWeight{
				{Weight: -1},
			},
		},
			valid: false},
		{name: "route rule no weight", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{Source: "somehost.default.svc.cluster.local"},
			Route: []*proxyconfig.DestinationWeight{
				{Destination: "host2.default.svc.cluster.local"},
			},
		},
			valid: true},
		{name: "route rule two destinationweights", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{Source: "somehost.default.svc.cluster.local"},
			Route: []*proxyconfig.DestinationWeight{
				{Destination: "host2.default.svc.cluster.local", Weight: 50},
				{Destination: "host3.default.svc.cluster.local", Weight: 50},
			},
		},
			valid: true},
		{name: "route rule two destinationweights", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{Source: "somehost.default.svc.cluster.local"},
			Route: []*proxyconfig.DestinationWeight{
				{Destination: "host.default.svc.cluster.local", Weight: 75, Tags: map[string]string{"version": "v1"}},
				{Destination: "host.default.svc.cluster.local", Weight: 25, Tags: map[string]string{"version": "v3"}},
			},
		},
			valid: true},
		{name: "route rule two destinationweights 99", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{Source: "somehost.default.svc.cluster.local"},
			Route: []*proxyconfig.DestinationWeight{
				{Destination: "host.default.svc.cluster.local", Weight: 75, Tags: map[string]string{"version": "v1"}},
				{Destination: "host.default.svc.cluster.local", Weight: 24, Tags: map[string]string{"version": "v3"}},
			},
		},
			valid: false},
		{name: "route rule bad route tags", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{Source: "somehost.default.svc.cluster.local"},
			Route: []*proxyconfig.DestinationWeight{
				{Tags: map[string]string{"@": "~"}},
			},
		},
			valid: false},
		{name: "route rule bad timeout", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			HttpReqTimeout: &proxyconfig.HTTPTimeout{
				TimeoutPolicy: &proxyconfig.HTTPTimeout_SimpleTimeout{
					SimpleTimeout: &proxyconfig.HTTPTimeout_SimpleTimeoutPolicy{
						Timeout: &duration.Duration{Seconds: -1}},
				},
			},
		},
			valid: false},
		{name: "route rule bad retry attempts", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			HttpReqRetries: &proxyconfig.HTTPRetry{
				RetryPolicy: &proxyconfig.HTTPRetry_SimpleRetry{
					SimpleRetry: &proxyconfig.HTTPRetry_SimpleRetryPolicy{
						Attempts: -1, PerTryTimeout: &duration.Duration{Seconds: 0}},
				},
			},
		},
			valid: false},
		{name: "route rule bad delay fixed seconds", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			HttpFault: &proxyconfig.HTTPFaultInjection{
				Delay: &proxyconfig.HTTPFaultInjection_Delay{
					Percent: -1,
					HttpDelayType: &proxyconfig.HTTPFaultInjection_Delay_FixedDelay{
						FixedDelay: &duration.Duration{Seconds: 3}},
				},
			},
		},
			valid: false},
		{name: "route rule bad delay fixed seconds", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			HttpFault: &proxyconfig.HTTPFaultInjection{
				Delay: &proxyconfig.HTTPFaultInjection_Delay{
					Percent: 100,
					HttpDelayType: &proxyconfig.HTTPFaultInjection_Delay_FixedDelay{
						FixedDelay: &duration.Duration{Seconds: -1}},
				},
			},
		},
			valid: false},
		{name: "route rule bad abort percent", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			HttpFault: &proxyconfig.HTTPFaultInjection{
				Abort: &proxyconfig.HTTPFaultInjection_Abort{
					Percent:   -1,
					ErrorType: &proxyconfig.HTTPFaultInjection_Abort_HttpStatus{HttpStatus: 500},
				},
			},
		},
			valid: false},
		{name: "route rule bad abort status", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			HttpFault: &proxyconfig.HTTPFaultInjection{
				Abort: &proxyconfig.HTTPFaultInjection_Abort{
					Percent:   100,
					ErrorType: &proxyconfig.HTTPFaultInjection_Abort_HttpStatus{HttpStatus: -1},
				},
			},
		},
			valid: false},
		{name: "route rule bad delay exp seconds", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			HttpFault: &proxyconfig.HTTPFaultInjection{
				Delay: &proxyconfig.HTTPFaultInjection_Delay{
					Percent: 101,
					HttpDelayType: &proxyconfig.HTTPFaultInjection_Delay_ExponentialDelay{
						ExponentialDelay: &duration.Duration{Seconds: -1}},
				},
			},
		},
			valid: false},
		{name: "route rule bad throttle after seconds", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			L4Fault: &proxyconfig.L4FaultInjection{
				Throttle: &proxyconfig.L4FaultInjection_Throttle{
					Percent:            101,
					DownstreamLimitBps: -1,
					UpstreamLimitBps:   -1,
					ThrottleAfter: &proxyconfig.L4FaultInjection_Throttle_ThrottleAfterPeriod{
						ThrottleAfterPeriod: &duration.Duration{Seconds: -1}},
				},
				Terminate: &proxyconfig.L4FaultInjection_Terminate{
					Percent:              101,
					TerminateAfterPeriod: &duration.Duration{Seconds: -1},
				},
			},
		},
			valid: false},
		{name: "route rule bad throttle after bytes", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			L4Fault: &proxyconfig.L4FaultInjection{
				Throttle: &proxyconfig.L4FaultInjection_Throttle{
					Percent:            101,
					DownstreamLimitBps: -1,
					UpstreamLimitBps:   -1,
					ThrottleAfter: &proxyconfig.L4FaultInjection_Throttle_ThrottleAfterBytes{
						ThrottleAfterBytes: -1},
				},
			},
		},
			valid: false},
		{name: "route rule bad match source tag label", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{SourceTags: map[string]string{"@": "0"}},
		},
			valid: false},
		{name: "route rule bad match source tag value", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{SourceTags: map[string]string{"a": "~"}},
		},
			valid: false},
		{name: "route rule match valid subnets", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match: &proxyconfig.MatchCondition{
				Tcp: &proxyconfig.L4MatchAttributes{
					SourceSubnet:      []string{"1.2.3.4"},
					DestinationSubnet: []string{"1.2.3.4/24"},
				},
			},
		},
			valid: true},
		{name: "route rule match invalid subnets", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match: &proxyconfig.MatchCondition{
				Tcp: &proxyconfig.L4MatchAttributes{
					SourceSubnet:      []string{"foo", "1.2.3.4/banana"},
					DestinationSubnet: []string{"1.2.3.4/500", "1.2.3.4/-1"},
				},
				Udp: &proxyconfig.L4MatchAttributes{
					SourceSubnet:      []string{"1.2.3.4", "1.2.3.4/24", ""},
					DestinationSubnet: []string{"foo.2.3.4", "1.2.3"},
				},
			},
		},
			valid: false},
		{name: "route rule match invalid redirect", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Redirect: &proxyconfig.HTTPRedirect{
				Uri:       "",
				Authority: "",
			},
		},
			valid: false},
		{name: "route rule match valid host redirect", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Redirect: &proxyconfig.HTTPRedirect{
				Authority: "foo.bar.com",
			},
		},
			valid: true},
		{name: "route rule match valid path redirect", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{Source: "somehost.default.svc.cluster.local"},
			Redirect: &proxyconfig.HTTPRedirect{
				Uri: "/new/path",
			},
		},
			valid: true},
		{name: "route rule match valid redirect", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Redirect: &proxyconfig.HTTPRedirect{
				Uri:       "/new/path",
				Authority: "foo.bar.com",
			},
		},
			valid: true},
		{name: "route rule match valid redirect", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Redirect: &proxyconfig.HTTPRedirect{
				Uri:       "/new/path",
				Authority: "foo.bar.com",
			},
			HttpFault: &proxyconfig.HTTPFaultInjection{},
		},
			valid: false},
		{name: "route rule match invalid redirect", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Redirect: &proxyconfig.HTTPRedirect{
				Uri: "/new/path",
			},
			Route: []*proxyconfig.DestinationWeight{
				{Destination: "host.default.svc.cluster.local", Weight: 75, Tags: map[string]string{"version": "v1"}},
				{Destination: "host.default.svc.cluster.local", Weight: 25, Tags: map[string]string{"version": "v3"}},
			},
		},
			valid: false},
		{name: "websocket upgrade invalid redirect", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Redirect: &proxyconfig.HTTPRedirect{
				Uri: "/new/path",
			},
			Route: []*proxyconfig.DestinationWeight{
				{Destination: "host.default.svc.cluster.local", Weight: 100},
			},
			WebsocketUpgrade: true,
		},
			valid: false},
		{name: "route rule match invalid rewrite", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Rewrite:     &proxyconfig.HTTPRewrite{},
		},
			valid: false},
		{name: "route rule match valid host rewrite", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Rewrite: &proxyconfig.HTTPRewrite{
				Authority: "foo.bar.com",
			},
		},
			valid: true},
		{name: "route rule match valid prefix rewrite", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Match:       &proxyconfig.MatchCondition{Source: "somehost.default.svc.cluster.local"},
			Rewrite: &proxyconfig.HTTPRewrite{
				Uri: "/new/path",
			},
		},
			valid: true},
		{name: "route rule match valid rewrite", in: &proxyconfig.RouteRule{
			Destination: "host.default.svc.cluster.local",
			Name:        "test",
			Rewrite: &proxyconfig.HTTPRewrite{
				Authority: "foo.bar.com",
				Uri:       "/new/path",
			},
		},
			valid: true},
	}
	for _, c := range cases {
		if got := ValidateRouteRule(c.in); (got == nil) != c.valid {
			t.Errorf("ValidateRouteRule failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}

func TestValidateDestinationPolicy(t *testing.T) {
	cases := []struct {
		in    proto.Message
		valid bool
	}{
		{in: &proxyconfig.RouteRule{}, valid: false},
		{in: &proxyconfig.DestinationPolicy{}, valid: false},
		{in: &proxyconfig.DestinationPolicy{Destination: "foobar"}, valid: true},
		{in: &proxyconfig.DestinationPolicy{
			Destination: "ratings!.default.svc.cluster.local",
			Policy: []*proxyconfig.DestinationVersionPolicy{{
				CircuitBreaker: &proxyconfig.CircuitBreaker{
					CbPolicy: &proxyconfig.CircuitBreaker_SimpleCb{
						SimpleCb: &proxyconfig.CircuitBreaker_SimpleCircuitBreakerPolicy{
							MaxConnections:               -1,
							HttpMaxPendingRequests:       -1,
							HttpMaxRequests:              -1,
							SleepWindow:                  &duration.Duration{Seconds: -1},
							HttpConsecutiveErrors:        -1,
							HttpDetectionInterval:        &duration.Duration{Seconds: -1},
							HttpMaxRequestsPerConnection: -1,
							HttpMaxEjectionPercent:       -1,
						},
					},
				}}},
		},
			valid: false},
		{in: &proxyconfig.DestinationPolicy{
			Destination: "ratings!.default.svc.cluster.local",
			Policy: []*proxyconfig.DestinationVersionPolicy{{
				CircuitBreaker: &proxyconfig.CircuitBreaker{
					CbPolicy: &proxyconfig.CircuitBreaker_SimpleCb{
						SimpleCb: &proxyconfig.CircuitBreaker_SimpleCircuitBreakerPolicy{
							HttpMaxEjectionPercent: 101,
						},
					},
				}}},
		},
			valid: false},
		{in: &proxyconfig.DestinationPolicy{
			Destination: "foobar",
			Policy: []*proxyconfig.DestinationVersionPolicy{{
				LoadBalancing: &proxyconfig.LoadBalancing{
					LbPolicy: &proxyconfig.LoadBalancing_Name{
						Name: 0,
					},
				}}},
		},
			valid: true},
		{in: &proxyconfig.DestinationPolicy{
			Destination: "foobar",
			Policy: []*proxyconfig.DestinationVersionPolicy{{
				Tags: map[string]string{"@": "~"},
			}},
		},
			valid: false},
	}
	for _, c := range cases {
		if got := ValidateDestinationPolicy(c.in); (got == nil) != c.valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%v: %v", got == nil, c.valid, got)
		}
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
		"istio-pilot:80":     true,
		"istio-pilot":        false,
		"isti..:80":          false,
		"10.0.0.100:9090":    true,
		"10.0.0.100":         false,
		"istio-pilot:port":   false,
		"istio-pilot:100000": false,
	}
	for addr, valid := range addresses {
		if got := ValidateProxyAddress(addr); (got == nil) != valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %s", got == nil, valid, got, addr)
		}
	}
}

func TestValidateDuration(t *testing.T) {
	durations := map[duration.Duration]bool{
		{Seconds: 1}:              true,
		{Seconds: 1, Nanos: -1}:   false,
		{Seconds: -11, Nanos: -1}: false,
		{Nanos: 1}:                false,
		{Seconds: 1, Nanos: 1}:    false,
	}
	for duration, valid := range durations {
		if got := ValidateDuration(&duration); (got == nil) != valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, valid, got, duration)
		}
	}
}

func TestValidateParentAndDrain(t *testing.T) {
	type ParentDrainTime struct {
		Parent duration.Duration
		Drain  duration.Duration
		Valid  bool
	}

	combinations := []ParentDrainTime{
		{
			Parent: duration.Duration{Seconds: 2},
			Drain:  duration.Duration{Seconds: 1},
			Valid:  true,
		},
		{
			Parent: duration.Duration{Seconds: 1},
			Drain:  duration.Duration{Seconds: 1},
			Valid:  false,
		},
		{
			Parent: duration.Duration{Seconds: 1},
			Drain:  duration.Duration{Seconds: 2},
			Valid:  false,
		},
		{
			Parent: duration.Duration{Seconds: 2},
			Drain:  duration.Duration{Seconds: 1, Nanos: 1},
			Valid:  false,
		},
		{
			Parent: duration.Duration{Seconds: 2, Nanos: 1},
			Drain:  duration.Duration{Seconds: 1},
			Valid:  false,
		},
		{
			Parent: duration.Duration{Seconds: -2},
			Drain:  duration.Duration{Seconds: 1},
			Valid:  false,
		},
		{
			Parent: duration.Duration{Seconds: 2},
			Drain:  duration.Duration{Seconds: -1},
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

func TestValidateRefreshDelay(t *testing.T) {
	durations := map[duration.Duration]bool{
		{Seconds: 1}:     true,
		{Seconds: 36001}: false,
		{Nanos: 1}:       false,
	}
	for duration, valid := range durations {
		if got := ValidateRefreshDelay(&duration); (got == nil) != valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, valid, got, duration)
		}
	}
}

func TestValidateConnectTimeout(t *testing.T) {
	durations := map[duration.Duration]bool{
		{Seconds: 1}:   true,
		{Seconds: 31}:  false,
		{Nanos: 99999}: false,
	}
	for duration, valid := range durations {
		if got := ValidateConnectTimeout(&duration); (got == nil) != valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, valid, got, duration)
		}
	}
}

func TestValidateProxyMeshConfig(t *testing.T) {
	invalid := proxyconfig.ProxyMeshConfig{
		EgressProxyAddress:     "10.0.0.100",
		DiscoveryAddress:       "10.0.0.100",
		MixerAddress:           "10.0.0.100",
		ProxyListenPort:        0,
		ProxyAdminPort:         0,
		DrainDuration:          ptypes.DurationProto(-1 * time.Second),
		ParentShutdownDuration: ptypes.DurationProto(-1 * time.Second),
		DiscoveryRefreshDelay:  ptypes.DurationProto(-1 * time.Second),
		ConnectTimeout:         ptypes.DurationProto(-1 * time.Second),
		IstioServiceCluster:    "",
		AuthPolicy:             -1,
		AuthCertsPath:          "",
		StatsdUdpAddress:       "10.0.0.100",
	}

	err := ValidateProxyMeshConfig(&invalid)
	if err == nil {
		t.Errorf("expected an error on invalid proxy mesh config: %v", invalid)
	} else {
		switch err.(type) {
		case *multierror.Error:
			// each field must cause an error in the field
			if len(err.(*multierror.Error).Errors) < 13 {
				t.Errorf("expected an error for each field %v", err)
			}
		default:
			t.Errorf("expected a multi error as output")
		}
	}
}

func TestValidateEgressRuleDomain(t *testing.T) {
	domains := map[string]bool{
		"cnn.com":    true,
		"cnn..com":   false,
		"10.0.0.100": true,
		"cnn.com:80": false,
		"*cnn.com":   true,
		"*.cnn.com":  true,
		"*-cnn.com":  true,
		"*.0.0.100":  true,
		"*0.0.100":   true,
		"*cnn*.com":  false,
		"cnn.*.com":  false,
		"*com":       true,
		"*0":         true,
		"**com":      false,
		"**0":        false,
		"*":          true,
		"":           false,
		"*.":         false,
	}

	for domain, valid := range domains {
		if got := ValidateEgressRuleDomain(domain); (got == nil) != valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %s", got == nil, valid, got, domain)
		}
	}
}

func TestValidateEgressRulePort(t *testing.T) {
	ports := map[*proxyconfig.EgressRule_Port]bool{
		{Port: 80, Protocol: "http"}:    true,
		{Port: 80, Protocol: "http2"}:   true,
		{Port: 80, Protocol: "grpc"}:    true,
		{Port: 443, Protocol: "https"}:  true,
		{Port: 80, Protocol: "https"}:   true,
		{Port: 443, Protocol: "http"}:   true,
		{Port: 1, Protocol: "http"}:     true,
		{Port: 2, Protocol: "https"}:    true,
		{Port: 80, Protocol: "tcp"}:     false,
		{Port: 0, Protocol: "http"}:     false,
		{Port: 65536, Protocol: "http"}: false,
		{Port: 65535, Protocol: "http"}: true,
	}

	for port, valid := range ports {
		if got := ValidateEgressRulePort(port); (got == nil) != valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, valid, got, port)
		}
	}
}

func TestValidateEgressRule(t *testing.T) {
	cases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{name: "empty egress rule", in: &proxyconfig.EgressRule{}, valid: false},
		{name: "valid egress rule",
			in: &proxyconfig.EgressRule{
				Name:    "cnn",
				Domains: []string{"*cnn.com", "*.cnn.com"},
				Ports: []*proxyconfig.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
				UseEgressProxy: false},
			valid: true},
		{name: "egress rule with use_egress_proxy = true, not yet implemented",
			in: &proxyconfig.EgressRule{
				Name:    "cnn",
				Domains: []string{"*cnn.com"},
				Ports: []*proxyconfig.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 8080, Protocol: "http"},
				},
				UseEgressProxy: true},
			valid: false},
		{name: "empty name",
			in: &proxyconfig.EgressRule{
				Name:    "",
				Domains: []string{"*cnn.com", "*.cnn.com"},
				Ports: []*proxyconfig.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
				UseEgressProxy: false},
			valid: false},
		{name: "empty domains",
			in: &proxyconfig.EgressRule{
				Name:    "cnn",
				Domains: []string{},
				Ports: []*proxyconfig.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
				UseEgressProxy: false},
			valid: false},
		{name: "empty ports",
			in: &proxyconfig.EgressRule{
				Name:           "cnn",
				Domains:        []string{"*cnn.com", "*.cnn.com"},
				Ports:          []*proxyconfig.EgressRule_Port{},
				UseEgressProxy: false},
			valid: false},
		{name: "duplicate domain",
			in: &proxyconfig.EgressRule{
				Name:    "cnn",
				Domains: []string{"*cnn.com", "*.cnn.com", "*cnn.com"},
				Ports: []*proxyconfig.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
				UseEgressProxy: false},
			valid: false},
		{name: "duplicate port",
			in: &proxyconfig.EgressRule{
				Name:    "cnn",
				Domains: []string{"*cnn.com", "*.cnn.com"},
				Ports: []*proxyconfig.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
					{Port: 80, Protocol: "https"},
				},
				UseEgressProxy: false},
			valid: false},
	}

	for _, c := range cases {
		if got := ValidateEgressRule(c.in); (got == nil) != c.valid {
			t.Errorf("ValidateEgressRule failed on %v: got valid=%v but wanted valid=%v: %v",
				c.name, got == nil, c.valid, got)
		}
	}
}
