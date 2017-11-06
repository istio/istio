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
	"github.com/golang/protobuf/ptypes/wrappers"
	multierror "github.com/hashicorp/go-multierror"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/istio/pilot/model/test"
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
			name:    "message type and kind mismatch",
			typ:     RouteRule.Type,
			config:  &proxyconfig.DestinationPolicy{},
			wantErr: true,
		},
		{
			name:    "ProtoSchema validation1",
			typ:     RouteRule.Type,
			config:  &proxyconfig.RouteRule{},
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
		if err := types.ValidateConfig(c.typ, c.config); (err != nil) != c.wantErr {
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

func TestValidateRouteAndIngressRule(t *testing.T) {
	cases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{name: "empty destination policy", in: &proxyconfig.DestinationPolicy{}, valid: false},
		{name: "empty route rule", in: &proxyconfig.RouteRule{}, valid: false},
		{name: "route rule w destination", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
		},
			valid: true},
		{name: "route rule bad destination", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar?"},
		},
			valid: false},
		{name: "route rule bad destination", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar", Labels: Labels{"version": "v1"}},
		},
			valid: false},
		{name: "route rule bad match source", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Match:       &proxyconfig.MatchCondition{Source: &proxyconfig.IstioService{Name: "somehost!"}},
		},
			valid: false},
		{name: "route rule bad weight", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Route: []*proxyconfig.DestinationWeight{
				{Weight: -1},
			},
		},
			valid: false},
		{name: "route rule no weight", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Route: []*proxyconfig.DestinationWeight{
				{Labels: map[string]string{"a": "b"}},
			},
		},
			valid: true},
		{name: "route rule two destinationweights", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Route: []*proxyconfig.DestinationWeight{
				{Labels: map[string]string{"a": "b"}, Weight: 50},
				{Labels: map[string]string{"a": "c"}, Weight: 50},
			},
		},
			valid: true},
		{name: "route rule two destinationweights 99", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Route: []*proxyconfig.DestinationWeight{
				{Labels: map[string]string{"a": "b"}, Weight: 50},
				{Labels: map[string]string{"a": "c"}, Weight: 49},
			},
		},
			valid: false},
		{name: "route rule bad route tags", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Route: []*proxyconfig.DestinationWeight{
				{Labels: map[string]string{"a": "?"}},
			},
		},
			valid: false},
		{name: "route rule bad timeout", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			HttpReqTimeout: &proxyconfig.HTTPTimeout{
				TimeoutPolicy: &proxyconfig.HTTPTimeout_SimpleTimeout{
					SimpleTimeout: &proxyconfig.HTTPTimeout_SimpleTimeoutPolicy{
						Timeout: &duration.Duration{Seconds: -1}},
				},
			},
		},
			valid: false},
		{name: "route rule bad retry attempts", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			HttpReqRetries: &proxyconfig.HTTPRetry{
				RetryPolicy: &proxyconfig.HTTPRetry_SimpleRetry{
					SimpleRetry: &proxyconfig.HTTPRetry_SimpleRetryPolicy{
						Attempts: -1, PerTryTimeout: &duration.Duration{Seconds: 0}},
				},
			},
		},
			valid: false},
		{name: "route rule bad delay fixed seconds", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
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
			Destination: &proxyconfig.IstioService{Name: "foobar"},
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
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			HttpFault: &proxyconfig.HTTPFaultInjection{
				Abort: &proxyconfig.HTTPFaultInjection_Abort{
					Percent:   -1,
					ErrorType: &proxyconfig.HTTPFaultInjection_Abort_HttpStatus{HttpStatus: 500},
				},
			},
		},
			valid: false},
		{name: "route rule bad abort status", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			HttpFault: &proxyconfig.HTTPFaultInjection{
				Abort: &proxyconfig.HTTPFaultInjection_Abort{
					Percent:   100,
					ErrorType: &proxyconfig.HTTPFaultInjection_Abort_HttpStatus{HttpStatus: -1},
				},
			},
		},
			valid: false},
		{name: "route rule bad unsupported status", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			HttpFault: &proxyconfig.HTTPFaultInjection{
				Abort: &proxyconfig.HTTPFaultInjection_Abort{
					Percent:   100,
					ErrorType: &proxyconfig.HTTPFaultInjection_Abort_GrpcStatus{GrpcStatus: "test"},
				},
			},
		},
			valid: false},
		{name: "route rule bad delay exp seconds", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
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
			Destination: &proxyconfig.IstioService{Name: "foobar"},
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
			Destination: &proxyconfig.IstioService{Name: "foobar"},
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
		{name: "route rule match valid subnets", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Match: &proxyconfig.MatchCondition{
				Tcp: &proxyconfig.L4MatchAttributes{
					SourceSubnet:      []string{"1.2.3.4"},
					DestinationSubnet: []string{"1.2.3.4/24"},
				},
			},
		},
			valid: true},
		{name: "route rule match invalid subnets", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
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
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Redirect: &proxyconfig.HTTPRedirect{
				Uri:       "",
				Authority: "",
			},
		},
			valid: false},
		{name: "route rule match valid host redirect", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Redirect: &proxyconfig.HTTPRedirect{
				Authority: "foo.bar.com",
			},
		},
			valid: true},
		{name: "route rule match valid path redirect", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Redirect: &proxyconfig.HTTPRedirect{
				Uri: "/new/path",
			},
		},
			valid: true},
		{name: "route rule match valid redirect", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Redirect: &proxyconfig.HTTPRedirect{
				Uri:       "/new/path",
				Authority: "foo.bar.com",
			},
		},
			valid: true},
		{name: "route rule match valid redirect", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Redirect: &proxyconfig.HTTPRedirect{
				Uri:       "/new/path",
				Authority: "foo.bar.com",
			},
			HttpFault: &proxyconfig.HTTPFaultInjection{},
		},
			valid: false},
		{name: "route rule match invalid redirect", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Redirect: &proxyconfig.HTTPRedirect{
				Uri: "/new/path",
			},
			Route: []*proxyconfig.DestinationWeight{
				{Labels: map[string]string{"version": "v1"}},
			},
		},
			valid: false},
		{name: "websocket upgrade invalid redirect", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Redirect: &proxyconfig.HTTPRedirect{
				Uri: "/new/path",
			},
			Route: []*proxyconfig.DestinationWeight{
				{Destination: &proxyconfig.IstioService{Name: "host"}, Weight: 100},
			},
			WebsocketUpgrade: true,
		},
			valid: false},
		{name: "route rule match invalid rewrite", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Rewrite:     &proxyconfig.HTTPRewrite{},
		},
			valid: false},
		{name: "route rule match valid host rewrite", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Rewrite: &proxyconfig.HTTPRewrite{
				Authority: "foo.bar.com",
			},
		},
			valid: true},
		{name: "route rule match rewrite and redirect", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Redirect:    &proxyconfig.HTTPRedirect{Uri: "/new/path"},
			Rewrite:     &proxyconfig.HTTPRewrite{Authority: "foo.bar.com"},
		},
			valid: false},
		{name: "route rule match valid prefix rewrite", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Rewrite: &proxyconfig.HTTPRewrite{
				Uri: "/new/path",
			},
		},
			valid: true},
		{name: "route rule match valid rewrite", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Rewrite: &proxyconfig.HTTPRewrite{
				Authority: "foo.bar.com",
				Uri:       "/new/path",
			},
		},
			valid: true},
		{name: "append headers", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			AppendHeaders: map[string]string{
				"name": "val",
			},
		},
			valid: true},
		{name: "append headers bad name", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			AppendHeaders: map[string]string{
				"": "val",
			},
		},
			valid: false},
		{name: "append headers bad val", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			AppendHeaders: map[string]string{
				"name": "",
			},
		},
			valid: false},
		{name: "mirror", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Mirror:      &proxyconfig.IstioService{Name: "barfoo"},
		},
			valid: true},
		{name: "mirror bad service", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			Mirror:      &proxyconfig.IstioService{},
		},
			valid: false},
		{name: "valid cors policy", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			CorsPolicy: &proxyconfig.CorsPolicy{
				MaxAge:           &duration.Duration{Seconds: 5},
				AllowOrigin:      []string{"http://foo.example"},
				AllowMethods:     []string{"POST", "GET", "OPTIONS"},
				AllowHeaders:     []string{"content-type"},
				AllowCredentials: &wrappers.BoolValue{Value: true},
				ExposeHeaders:    []string{"x-custom-header"},
			},
		},
			valid: true},
		{name: "cors policy invalid allow headers", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			CorsPolicy: &proxyconfig.CorsPolicy{
				MaxAge:           &duration.Duration{Seconds: 5},
				AllowOrigin:      []string{"http://foo.example"},
				AllowMethods:     []string{"POST", "GET", "OPTIONS"},
				AllowHeaders:     []string{"Content-Type"},
				AllowCredentials: &wrappers.BoolValue{Value: true},
				ExposeHeaders:    []string{"x-custom-header"},
			},
		},
			valid: false},
		{name: "cors policy invalid expose headers", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			CorsPolicy: &proxyconfig.CorsPolicy{
				MaxAge:           &duration.Duration{Seconds: 5},
				AllowOrigin:      []string{"http://foo.example"},
				AllowMethods:     []string{"POST", "GET", "OPTIONS"},
				AllowHeaders:     []string{"content-type"},
				AllowCredentials: &wrappers.BoolValue{Value: true},
				ExposeHeaders:    []string{"X-Custom-Header"},
			},
		},
			valid: false},
		{name: "invalid cors policy bad max age", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			CorsPolicy: &proxyconfig.CorsPolicy{
				MaxAge: &duration.Duration{Nanos: 1000000},
			},
		},
			valid: false},
		{name: "invalid cors policy invalid max age", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			CorsPolicy: &proxyconfig.CorsPolicy{
				MaxAge: &duration.Duration{Nanos: 100},
			},
		},
			valid: false},
		{name: "invalid cors policy bad allow method", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			CorsPolicy: &proxyconfig.CorsPolicy{
				MaxAge:           &duration.Duration{Seconds: 5},
				AllowOrigin:      []string{"http://foo.example"},
				AllowMethods:     []string{"POST", "GET", "UNSUPPORTED"},
				AllowHeaders:     []string{"content-type"},
				AllowCredentials: &wrappers.BoolValue{Value: true},
				ExposeHeaders:    []string{"x-custom-header"},
			},
		},
			valid: false},
		{name: "invalid cors policy bad allow method 2", in: &proxyconfig.RouteRule{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			CorsPolicy: &proxyconfig.CorsPolicy{
				MaxAge:           &duration.Duration{Seconds: 5},
				AllowOrigin:      []string{"http://foo.example"},
				AllowMethods:     []string{"POST", "get"},
				AllowHeaders:     []string{"content-type"},
				AllowCredentials: &wrappers.BoolValue{Value: true},
				ExposeHeaders:    []string{"x-custom-header"},
			},
		},
			valid: false},
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
		{in: &proxyconfig.DestinationPolicy{Destination: &proxyconfig.IstioService{Name: "foobar"}}, valid: true},
		{in: &proxyconfig.DestinationPolicy{
			Destination: &proxyconfig.IstioService{
				Name:      "?",
				Namespace: "?",
				Domain:    "a.?",
			},
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
			},
		},
			valid: false},
		{in: &proxyconfig.DestinationPolicy{
			Destination: &proxyconfig.IstioService{Name: "ratings"},
			CircuitBreaker: &proxyconfig.CircuitBreaker{
				CbPolicy: &proxyconfig.CircuitBreaker_SimpleCb{
					SimpleCb: &proxyconfig.CircuitBreaker_SimpleCircuitBreakerPolicy{
						HttpMaxEjectionPercent: 101,
					},
				},
			},
		},
			valid: false},
		{in: &proxyconfig.DestinationPolicy{
			Destination: &proxyconfig.IstioService{Name: "foobar"},
			LoadBalancing: &proxyconfig.LoadBalancing{
				LbPolicy: &proxyconfig.LoadBalancing_Name{
					Name: 0,
				},
			},
		},
			valid: true},
		{in: &proxyconfig.DestinationPolicy{
			Destination:   &proxyconfig.IstioService{Name: "foobar"},
			LoadBalancing: &proxyconfig.LoadBalancing{},
		},
			valid: false},
		{in: &proxyconfig.DestinationPolicy{
			Source: &proxyconfig.IstioService{},
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
			Drain:  duration.Duration{Seconds: 1, Nanos: 1000000},
			Valid:  false,
		},
		{
			Parent: duration.Duration{Seconds: 2, Nanos: 1000000},
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
		{
			Parent: duration.Duration{Seconds: 1 + int64(time.Hour/time.Second)},
			Drain:  duration.Duration{Seconds: 10},
			Valid:  false,
		},
		{
			Parent: duration.Duration{Seconds: 10},
			Drain:  duration.Duration{Seconds: 1 + int64(time.Hour/time.Second)},
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

func TestValidateMeshConfig(t *testing.T) {
	if ValidateMeshConfig(&proxyconfig.MeshConfig{}) == nil {
		t.Error("expected an error on an empty mesh config")
	}

	invalid := proxyconfig.MeshConfig{
		EgressProxyAddress: "10.0.0.100",
		MixerAddress:       "10.0.0.100",
		ProxyListenPort:    0,
		ConnectTimeout:     ptypes.DurationProto(-1 * time.Second),
		AuthPolicy:         -1,
		RdsRefreshDelay:    ptypes.DurationProto(-1 * time.Second),
		DefaultConfig:      &proxyconfig.ProxyConfig{},
	}

	err := ValidateMeshConfig(&invalid)
	if err == nil {
		t.Errorf("expected an error on invalid proxy mesh config: %v", invalid)
	} else {
		switch err.(type) {
		case *multierror.Error:
			// each field must cause an error in the field
			if len(err.(*multierror.Error).Errors) < 6 {
				t.Errorf("expected an error for each field %v", err)
			}
		default:
			t.Errorf("expected a multi error as output")
		}
	}
}

func TestValidateProxyConfig(t *testing.T) {
	if ValidateProxyConfig(&proxyconfig.ProxyConfig{}) == nil {
		t.Error("expected an error on an empty proxy config")
	}

	invalid := proxyconfig.ProxyConfig{
		ConfigPath:             "",
		BinaryPath:             "",
		DiscoveryAddress:       "10.0.0.100",
		ProxyAdminPort:         0,
		DrainDuration:          ptypes.DurationProto(-1 * time.Second),
		ParentShutdownDuration: ptypes.DurationProto(-1 * time.Second),
		DiscoveryRefreshDelay:  ptypes.DurationProto(-1 * time.Second),
		ConnectTimeout:         ptypes.DurationProto(-1 * time.Second),
		ServiceCluster:         "",
		StatsdUdpAddress:       "10.0.0.100",
		ZipkinAddress:          "10.0.0.100",
		ControlPlaneAuthPolicy: -1,
	}

	err := ValidateProxyConfig(&invalid)
	if err == nil {
		t.Errorf("expected an error on invalid proxy mesh config: %v", invalid)
	} else {
		switch err.(type) {
		case *multierror.Error:
			// each field must cause an error in the field
			if len(err.(*multierror.Error).Errors) < 12 {
				t.Errorf("expected an error for each field %v", err)
			}
		default:
			t.Errorf("expected a multi error as output")
		}
	}
}

func TestValidateIstioService(t *testing.T) {
	type IstioService struct {
		Service proxyconfig.IstioService
		Valid   bool
	}

	services := []IstioService{
		{
			Service: proxyconfig.IstioService{Name: "", Service: "", Domain: "", Namespace: ""},
			Valid:   false,
		},
		{
			Service: proxyconfig.IstioService{Service: "**cnn.com"},
			Valid:   false,
		},
		{
			Service: proxyconfig.IstioService{Service: "cnn.com", Labels: Labels{"*": ":"}},
			Valid:   false,
		},
		{
			Service: proxyconfig.IstioService{Service: "*cnn.com", Domain: "domain", Namespace: "namespace"},
			Valid:   false,
		},
		{
			Service: proxyconfig.IstioService{Service: "*cnn.com", Namespace: "namespace"},
			Valid:   false,
		},
		{
			Service: proxyconfig.IstioService{Service: "*cnn.com", Domain: "domain"},
			Valid:   false,
		},
		{
			Service: proxyconfig.IstioService{Name: "name", Service: "*cnn.com"},
			Valid:   false,
		},
		{
			Service: proxyconfig.IstioService{Service: "*cnn.com"},
			Valid:   true,
		},
		{
			Service: proxyconfig.IstioService{Name: "reviews", Domain: "svc.local", Namespace: "default"},
			Valid:   true,
		},
		{
			Service: proxyconfig.IstioService{Name: "reviews", Domain: "default", Namespace: "svc.local"},
			Valid:   false,
		},
	}

	for _, svc := range services {
		if got := ValidateIstioService(&svc.Service); (got == nil) != svc.Valid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %s",
				got == nil, svc.Valid, got, svc.Service)
		}
	}
}

func TestValidateMatchCondition(t *testing.T) {
	for key, mc := range map[string]*proxyconfig.MatchCondition{
		"bad header key": {
			Request: &proxyconfig.MatchRequest{Headers: map[string]*proxyconfig.StringMatch{
				"XHeader": {MatchType: &proxyconfig.StringMatch_Exact{Exact: "test"}},
			}},
		},
		"bad header value": {
			Request: &proxyconfig.MatchRequest{Headers: map[string]*proxyconfig.StringMatch{
				"user-agent": {},
			}},
		},
		"uri header exact empty": {
			Request: &proxyconfig.MatchRequest{Headers: map[string]*proxyconfig.StringMatch{
				HeaderURI: {MatchType: &proxyconfig.StringMatch_Exact{}},
			}},
		},
		"uri header prefix empty": {
			Request: &proxyconfig.MatchRequest{Headers: map[string]*proxyconfig.StringMatch{
				HeaderURI: {MatchType: &proxyconfig.StringMatch_Prefix{}},
			}},
		},
		"uri header regex empty": {
			Request: &proxyconfig.MatchRequest{Headers: map[string]*proxyconfig.StringMatch{
				HeaderURI: {MatchType: &proxyconfig.StringMatch_Regex{}},
			}},
		},
	} {
		if ValidateMatchCondition(mc) == nil {
			t.Errorf("expected error %s for %#v", key, mc)
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
		{Port: 80, Protocol: "tcp"}:     true,
		{Port: 80, Protocol: "udp"}:     false,
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

func TestValidateIngressRule(t *testing.T) {
	cases := []struct {
		name string
		in   proto.Message
	}{
		{name: "nil egress rule"},
		{name: "empty egress rule", in: &proxyconfig.IngressRule{}},
		{name: "empty egress rule", in: &proxyconfig.IngressRule{
			Destination: &proxyconfig.IstioService{
				Service: "***", Labels: Labels{"version": "v1"},
			},
		}},
	}

	for _, c := range cases {
		if ValidateIngressRule(c.in) == nil {
			t.Errorf("ValidateIngressRule failed on %s: %#v", c.name, c.in)
		}
	}
}

func TestValidateEgressRule(t *testing.T) {
	cases := []struct {
		name  string
		in    proto.Message
		valid bool
	}{
		{name: "nil egress rule"},
		{name: "empty egress rule", in: &proxyconfig.EgressRule{}, valid: false},
		{name: "valid egress rule",
			in: &proxyconfig.EgressRule{
				Destination: &proxyconfig.IstioService{
					Service: "*cnn.com",
				},
				Ports: []*proxyconfig.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
				UseEgressProxy: false},
			valid: true},
		{name: "egress rule with use_egress_proxy = true, not yet implemented",
			in: &proxyconfig.EgressRule{
				Destination: &proxyconfig.IstioService{
					Service: "*cnn.com",
				},
				Ports: []*proxyconfig.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 8080, Protocol: "http"},
				},
				UseEgressProxy: true},
			valid: false},
		{name: "empty destination",
			in: &proxyconfig.EgressRule{
				Destination: &proxyconfig.IstioService{},
				Ports: []*proxyconfig.EgressRule_Port{
					{Port: 80, Protocol: "http"},
					{Port: 443, Protocol: "https"},
				},
				UseEgressProxy: false},
			valid: false},
		{name: "empty ports",
			in: &proxyconfig.EgressRule{
				Destination: &proxyconfig.IstioService{
					Service: "*cnn.com",
				},
				Ports:          []*proxyconfig.EgressRule_Port{},
				UseEgressProxy: false},
			valid: false},
		{name: "duplicate port",
			in: &proxyconfig.EgressRule{
				Destination: &proxyconfig.IstioService{
					Service: "*cnn.com",
				},
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
