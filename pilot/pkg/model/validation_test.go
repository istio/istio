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
		{"IP address", "1.1.1.1", ""},
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

func TestValidateRefreshDelay(t *testing.T) {
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
			duration: &types.Duration{Seconds: 36001},
			isValid:  false,
		},
		{
			duration: &types.Duration{Nanos: 1},
			isValid:  false,
		},
	}

	for _, check := range checks {
		if got := ValidateRefreshDelay(check.duration); (got == nil) != check.isValid {
			t.Errorf("Failed: got valid=%t but wanted valid=%t: %v for %v", got == nil, check.isValid, got, check.duration)
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
		AuthPolicy:        -1,
		RdsRefreshDelay:   types.DurationProto(-1 * time.Second),
		DefaultConfig:     &meshconfig.ProxyConfig{},
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
	if ValidateProxyConfig(&meshconfig.ProxyConfig{}) == nil {
		t.Error("expected an error on an empty proxy config")
	}

	invalid := meshconfig.ProxyConfig{
		ConfigPath:             "",
		BinaryPath:             "",
		DiscoveryAddress:       "10.0.0.100",
		ProxyAdminPort:         0,
		DrainDuration:          types.DurationProto(-1 * time.Second),
		ParentShutdownDuration: types.DurationProto(-1 * time.Second),
		DiscoveryRefreshDelay:  types.DurationProto(-1 * time.Second),
		ConnectTimeout:         types.DurationProto(-1 * time.Second),
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
				ServerCertificate: "Captain Jean-Luc Picard"},
			""},
		{"simple with client bundle",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_SIMPLE,
				ServerCertificate: "Captain Jean-Luc Picard",
				CaCertificates:    "Commander William T. Riker"},
			""},
		{"simple no server cert",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_SIMPLE,
				ServerCertificate: ""},
			"server certificate"},
		{"mutual",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				CaCertificates:    "Commander William T. Riker"},
			""},
		{"mutual no server cert",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "",
				CaCertificates:    "Commander William T. Riker"},
			"server certificate"},
		{"mutual no client CA bundle",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "Captain Jean-Luc Picard",
				CaCertificates:    ""},
			"client CA bundle"},
		// this pair asserts we get errors about both client and server certs missing when in mutual mode
		// and both are absent, but requires less rewriting of the testing harness than merging the cases
		{"mutual no certs",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "",
				CaCertificates:    ""},
			"server certificate"},
		{"mutual no certs",
			&networking.Server_TLSOptions{
				Mode:              networking.Server_TLSOptions_MUTUAL,
				ServerCertificate: "",
				CaCertificates:    ""},
			"client CA bundle"},
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
		{name: "HEADER2", valid: false},
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
			ExposeHeaders: []string{"HEADER3"},
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
		}, valid: true},
		{name: "valid default", in: &networking.HTTPRetry{
			Attempts: 10,
		}, valid: true},
		{name: "bad attempts", in: &networking.HTTPRetry{
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
		{name: "uri and authority", in: &networking.HTTPRewrite{
			Uri:       "/path/to/resource",
			Authority: "foobar.org",
		}, valid: true},
		{name: "uri", in: &networking.HTTPRewrite{
			Uri: "/path/to/resource",
		}, valid: true},
		{name: "authority", in: &networking.HTTPRewrite{
			Authority: "foobar.org",
		}, valid: true},
		{name: "no uri or authority", in: &networking.HTTPRewrite{}, valid: false},
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

func TestValidateDestination(t *testing.T) {
	testCases := []struct {
		name        string
		destination *networking.Destination
		valid       bool
	}{
		{name: "empty", destination: &networking.Destination{ // nothing
		}, valid:                                             false},
		{name: "simple", destination: &networking.Destination{
			Host: "foo.bar",
		}, valid: true},
		{name: "full", destination: &networking.Destination{
			Host:   "foo.bar",
			Subset: "shiny",
			Port:   &networking.PortSelector{Port: &networking.PortSelector_Number{Number: 5000}},
		}, valid: true},
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
			Route: []*networking.DestinationWeight{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
		}, valid: true},
		{name: "no destination", route: &networking.HTTPRoute{
			Route: []*networking.DestinationWeight{{
				Destination: nil,
			}},
		}, valid: false},
		{name: "weighted", route: &networking.HTTPRoute{
			Route: []*networking.DestinationWeight{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      25,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      75,
			}},
		}, valid: true},
		{name: "total weight > 100", route: &networking.HTTPRoute{
			Route: []*networking.DestinationWeight{{
				Destination: &networking.Destination{Host: "foo.baz.south"},
				Weight:      55,
			}, {
				Destination: &networking.Destination{Host: "foo.baz.east"},
				Weight:      50,
			}},
		}, valid: false},
		{name: "total weight < 100", route: &networking.HTTPRoute{
			Route: []*networking.DestinationWeight{{
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
			Route: []*networking.DestinationWeight{{
				Destination: &networking.Destination{Host: "foo.baz"},
			}},
			Redirect: &networking.HTTPRedirect{
				Uri:       "/lerp",
				Authority: "foo.biz",
			},
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
				Route: []*networking.DestinationWeight{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true},
		{name: "duplicate hosts", in: &networking.VirtualService{
			Hosts: []string{"*.foo.bar", "*.bar"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.DestinationWeight{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "no hosts", in: &networking.VirtualService{
			Hosts: nil,
			Http: []*networking.HTTPRoute{{
				Route: []*networking.DestinationWeight{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "bad host", in: &networking.VirtualService{
			Hosts: []string{"foo.ba!r"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.DestinationWeight{{
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
				Route: []*networking.DestinationWeight{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "FQDN for gateway", in: &networking.VirtualService{
			Hosts:    []string{"foo.bar"},
			Gateways: []string{"gateway.example.com"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.DestinationWeight{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: true},
		{name: "wildcard for mesh gateway", in: &networking.VirtualService{
			Hosts: []string{"*"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.DestinationWeight{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
			}},
		}, valid: false},
		{name: "wildcard for non-mesh gateway", in: &networking.VirtualService{
			Hosts:    []string{"*"},
			Gateways: []string{"somegateway"},
			Http: []*networking.HTTPRoute{{
				Route: []*networking.DestinationWeight{{
					Destination: &networking.Destination{Host: "foo.baz"},
				}},
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
	}

	for _, c := range cases {
		if got := validateConnectionPool(&c.in); (got == nil) != c.valid {
			t.Errorf("ValidateConnectionSettings failed on %v: got valid=%v but wanted valid=%v: %v",
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
				{Address: "in.google.com", Ports: map[string]uint32{"http-valid2": 9080}},
			},
			Resolution: networking.ServiceEntry_DNS,
		},
			valid: false},
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
			name: "no service",
			in: &rbac.ServiceRole{Rules: []*rbac.AccessRule{
				{
					Services: []string{"service0"},
					Methods:  []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
				{
					Services: []string{},
					Methods:  []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
			}},
			expectErrMsg: "at least 1 service must be specified for rule 1",
		},
		{
			name: "no method",
			in: &rbac.ServiceRole{Rules: []*rbac.AccessRule{
				{
					Services: []string{"service0"},
					Methods:  []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
				{
					Services: []string{"service0"},
					Methods:  []string{},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
			}},
			expectErrMsg: "at least 1 method must be specified for rule 1",
		},
		{
			name: "no key in constraint",
			in: &rbac.ServiceRole{Rules: []*rbac.AccessRule{
				{
					Services: []string{"service0"},
					Methods:  []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
				{
					Services: []string{"service0"},
					Methods:  []string{"GET", "POST"},
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
					Services: []string{"service0"},
					Methods:  []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
				{
					Services: []string{"service0"},
					Methods:  []string{"GET", "POST"},
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
					Services: []string{"service0"},
					Methods:  []string{"GET", "POST"},
					Constraints: []*rbac.AccessRule_Constraint{
						{Key: "key", Values: []string{"value"}},
						{Key: "key", Values: []string{"value"}},
					},
				},
				{
					Services: []string{"service0"},
					Methods:  []string{"GET", "POST"},
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
			expectErrMsg: "at least 1 of user, group or properties must be specified for subject 1",
		},
		{
			name: "no roleRef",
			in: &rbac.ServiceRoleBinding{
				Subjects: []*rbac.Subject{
					{User: "User0", Group: "Group0", Properties: map[string]string{"prop0": "value0"}},
					{User: "User1", Group: "Group1", Properties: map[string]string{"prop1": "value1"}},
				},
			},
			expectErrMsg: "roleRef must be specified",
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
				RoleRef: &rbac.RoleRef{Kind: "ServiceRole", Name: ""},
			},
			expectErrMsg: "name cannot be empty",
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

func TestValidateRbacConfig(t *testing.T) {
	cases := []struct {
		caseName     string
		name         string
		namespace    string
		in           proto.Message
		expectErrMsg string
	}{
		{
			caseName:     "invalid proto",
			expectErrMsg: "cannot cast to RbacConfig",
		},
		{
			caseName: "invalid name",
			name:     "Rbac-config",
			in:       &rbac.RbacConfig{Mode: rbac.RbacConfig_ON_WITH_INCLUSION},
			expectErrMsg: fmt.Sprintf("rbacConfig has invalid name(Rbac-config), name must be %s",
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
		err := ValidateRbacConfig(c.name, c.namespace, c.in)
		if err == nil {
			if len(c.expectErrMsg) != 0 {
				t.Errorf("ValidateRbacConfig(%v): got nil but want %q\n", c.caseName, c.expectErrMsg)
			}
		} else if err.Error() != c.expectErrMsg {
			t.Errorf("ValidateRbacConfig(%v): got %q but want %q\n", c.caseName, err.Error(), c.expectErrMsg)
		}
	}
}
