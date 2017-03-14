// Keep this in sync with the `//model:genmock` build target.
//go:generate mockgen -source config.go -destination mock_config_gen_test.go -package model

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
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/mock/gomock"
	"github.com/golang/protobuf/proto"

	proxyconfig "istio.io/manager/model/proxy/alphav1/config"
)

var (
	validKeys = []Key{
		{Kind: "my-config", Name: "example-config-name", Namespace: "default"},
		{Kind: "my-config", Name: "x", Namespace: "default"},
		{Kind: "some-kind", Name: "x", Namespace: "default"},
	}
	invalidKeys = []Key{
		{Kind: "my-config", Name: "exampleConfigName", Namespace: "default"},
		{Name: "x"},
		{Kind: "my-config", Name: "x"},
		{Kind: "ExampleKind", Name: "x", Namespace: "default"},
	}
)

func TestKeyValidate(t *testing.T) {
	for _, valid := range validKeys {
		if err := valid.Validate(); err != nil {
			t.Errorf("Valid config failed validation: %#v", valid)
		}
	}
	for _, invalid := range invalidKeys {
		if err := invalid.Validate(); err == nil {
			t.Errorf("Invalid config passed validation: %#v", invalid)
		}
	}
}

func TestKindMapValidate(t *testing.T) {
	badLabel := strings.Repeat("a", dns1123LabelMaxLength+1)
	goodLabel := strings.Repeat("a", dns1123LabelMaxLength-1)

	cases := []struct {
		name    string
		kindMap KindMap
		wantErr bool
	}{{
		name:    "Valid KindMap (IstioConfig)",
		kindMap: IstioConfig,
		wantErr: false,
	}, {
		name:    "Invalid DNS11234Label in KindMap",
		kindMap: KindMap{badLabel: ProtoSchema{}},
		wantErr: true,
	}, {
		name:    "Bad MessageName in ProtoMessage",
		kindMap: KindMap{goodLabel: ProtoSchema{}},
		wantErr: true,
	}}

	for _, c := range cases {
		if err := c.kindMap.Validate(); (err != nil) != c.wantErr {
			t.Errorf("%v failed: got %v but wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func TestKindMapValidKey(t *testing.T) {
	cases := []struct {
		name    string
		key     Key
		kindMap KindMap
		wantErr bool
	}{
		{
			name:    "Valid key that exists in KindMap",
			key:     validKeys[0],
			kindMap: KindMap{validKeys[0].Kind: ProtoSchema{}},
			wantErr: false,
		},
		{
			name:    "Valid key that doesn't exists in KindMap",
			key:     validKeys[0],
			kindMap: KindMap{},
			wantErr: true,
		},
		{
			name:    "InvValid key that exists in KindMap",
			key:     invalidKeys[0],
			kindMap: KindMap{validKeys[0].Kind: ProtoSchema{}},
			wantErr: true,
		},
		{
			name:    "Invalid key that doesn't exists in KindMap",
			key:     invalidKeys[0],
			kindMap: KindMap{},
			wantErr: true,
		},
	}
	for _, c := range cases {
		if err := c.kindMap.ValidateKey(&c.key); (err != nil) != c.wantErr {
			t.Errorf("%v  failed got error=%v but wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func TestKindMapValidateConfig(t *testing.T) {
	cases := []struct {
		name    string
		key     *Key
		config  interface{}
		wantErr bool
	}{
		{
			name:    "bad key",
			key:     nil,
			config:  &proxyconfig.RouteRule{},
			wantErr: true,
		},
		{
			name:    "bad configuration object",
			key:     &validKeys[0],
			config:  nil,
			wantErr: true,
		},
		{
			name:    "invalid key",
			key:     &invalidKeys[0],
			config:  &proxyconfig.RouteRule{},
			wantErr: true,
		},
		{
			name:    "undeclared kind",
			key:     &validKeys[0],
			config:  &proxyconfig.RouteRule{},
			wantErr: true,
		},
		{
			name: "non-proto object configuration",
			key: &Key{
				Kind:      RouteRule,
				Name:      "foo",
				Namespace: "bar",
			},
			config:  "non-proto objection configuration",
			wantErr: true,
		},
		{
			name: "message type and kind mismatch",
			key: &Key{
				Kind:      RouteRule,
				Name:      "foo",
				Namespace: "bar",
			},
			config: &proxyconfig.DestinationPolicy{
				Destination: "foo",
			},
			wantErr: true,
		},
		{
			name: "ProtoSchema validation",
			key: &Key{
				Kind:      RouteRule,
				Name:      "foo",
				Namespace: "bar",
			},
			config:  &proxyconfig.RouteRule{},
			wantErr: true,
		},
		{
			name: "ProtoSchema validation",
			key: &Key{
				Kind:      RouteRule,
				Name:      "foo",
				Namespace: "bar",
			},
			config: &proxyconfig.RouteRule{
				Destination: "foo",
			},
			wantErr: false,
		},
	}

	for _, c := range cases {
		if err := IstioConfig.ValidateConfig(c.key, c.config); (err != nil) != c.wantErr {
			t.Errorf("%v failed: got error=%v but wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func TestKindMapKinds(t *testing.T) {
	km := KindMap{
		"b": ProtoSchema{},
		"a": ProtoSchema{},
		"c": ProtoSchema{},
	}
	want := []string{"a", "b", "c"}
	got := km.Kinds()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("KindMap.Kinds failed: \ngot %+vwant %+v", spew.Sdump(got), spew.Sdump(want))
	}
}

type testRegistry struct {
	ctrl     gomock.Controller
	mock     *MockConfigRegistry
	registry IstioRegistry
}

func initTestRegistry(t *testing.T) *testRegistry {
	ctrl := gomock.NewController(t)
	mock := NewMockConfigRegistry(ctrl)
	return &testRegistry{
		mock: mock,
		registry: IstioRegistry{
			ConfigRegistry: mock,
		},
	}
}

func (r *testRegistry) shutdown() {
	r.ctrl.Finish()
}

var (
	endpoint1 = NetworkEndpoint{
		Address:     "192.168.1.1",
		Port:        10001,
		ServicePort: &Port{Name: "http", Port: 81, Protocol: ProtocolHTTP},
	}
	endpoint2 = NetworkEndpoint{
		Address:     "192.168.1.2",
		Port:        10002,
		ServicePort: &Port{Name: "http", Port: 82, Protocol: ProtocolHTTP},
	}

	service1 = &Service{
		Hostname: "one.service.com",
		Address:  "192.168.3.1", // VIP
		Ports: PortList{
			&Port{Name: "http", Port: 81, Protocol: ProtocolHTTP},
			&Port{Name: "http-alt", Port: 8081, Protocol: ProtocolHTTP},
		},
	}
	service2 = &Service{
		Hostname: "two.service.com",
		Address:  "192.168.3.2", // VIP
		Ports: PortList{
			&Port{Name: "http", Port: 82, Protocol: ProtocolHTTP},
			&Port{Name: "http-alt", Port: 8282, Protocol: ProtocolHTTP},
		},
	}

	serviceInstance1 = &ServiceInstance{
		Endpoint: endpoint1,
		Service:  service1,
		Tags:     Tags{"a": "b", "c": "d"},
	}
	serviceInstance2 = &ServiceInstance{
		Endpoint: endpoint2,

		Service: service2,

		Tags: Tags{"e": "f", "g": "h"},
	}

	routeRule1MatchNil = &proxyconfig.RouteRule{
		Destination: "foo",
		Precedence:  1,
	}

	routeRule2SourceEmpty = &proxyconfig.RouteRule{
		Destination: "foo",
		Precedence:  1,
		Match:       &proxyconfig.MatchCondition{},
	}
	routeRule3SourceMismatch = &proxyconfig.RouteRule{
		Destination: "foo",
		Precedence:  3,
		Match: &proxyconfig.MatchCondition{
			Source: "three.service.com",
		},
	}
	routeRule4SourceMatch = &proxyconfig.RouteRule{
		Destination: "foo",
		Precedence:  4,
		Match: &proxyconfig.MatchCondition{
			Source: "one.service.com",
		},
	}
	routeRule5TagSubsetOfMismatch = &proxyconfig.RouteRule{
		Destination: "foo",
		Precedence:  5,
		Match: &proxyconfig.MatchCondition{
			Source:     "two.service.com",
			SourceTags: map[string]string{"z": "y"},
		},
	}
	routeRule6TagSubsetOfMatch = &proxyconfig.RouteRule{
		Destination: "foo",
		Precedence:  5,
		Match: &proxyconfig.MatchCondition{
			Source:     "one.service.com",
			SourceTags: map[string]string{"a": "b"},
		},
	}

	dstTags0 = map[string]string{"a": "b"}
	dstTags1 = map[string]string{"c": "d"}
	dstTags2 = map[string]string{"e": "f"}

	dstPolicy1 = &proxyconfig.DestinationPolicy{
		Destination: "foo",
		Tags:        dstTags0,
	}
	dstPolicy2 = &proxyconfig.DestinationPolicy{
		Destination: "foo",
	}
	dstPolicy3 = &proxyconfig.DestinationPolicy{
		Destination: "bar",
		Tags:        dstTags1,
	}
	dstPolicy4 = &proxyconfig.DestinationPolicy{
		Destination: "baz",
		Tags:        dstTags2,
	}
)

func TestIstioRegistryRouteAndIngressRules(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	cases := []struct {
		name      string
		mockError error
		mockObjs  map[Key]proto.Message
		want      map[Key]*proxyconfig.RouteRule
	}{
		{
			name:      "Empty object map with error",
			mockObjs:  map[Key]proto.Message{},
			mockError: errors.New("foobar"),
			want:      map[Key]*proxyconfig.RouteRule{},
		},
		{
			name: "Slice of unsorted RouteRules",
			mockObjs: map[Key]proto.Message{
				{Name: "foo"}: routeRule1MatchNil,
				{Name: "bar"}: routeRule3SourceMismatch,
				{Name: "baz"}: routeRule2SourceEmpty,
			},
			want: map[Key]*proxyconfig.RouteRule{
				{Name: "foo"}: routeRule1MatchNil,
				{Name: "bar"}: routeRule3SourceMismatch,
				{Name: "baz"}: routeRule2SourceEmpty,
			},
		},
	}
	for _, c := range cases {
		r.mock.EXPECT().List(RouteRule, "").Return(c.mockObjs, c.mockError)
		if got := r.registry.RouteRules(""); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v with RouteRule failed: \ngot %+vwant %+v", c.name, spew.Sdump(got), spew.Sdump(c.want))
		}
		r.mock.EXPECT().List(IngressRule, "").Return(c.mockObjs, c.mockError)
		if got := r.registry.IngressRules(""); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v with IngressRule failed: \ngot %+vwant %+v", c.name, spew.Sdump(got), spew.Sdump(c.want))
		}
	}
}

func TestIstioRegistryRouteRulesBySource(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	instances := []*ServiceInstance{serviceInstance1, serviceInstance2}

	mockObjs := map[Key]proto.Message{
		{Name: "match-nil"}:              routeRule1MatchNil,
		{Name: "source-empty"}:           routeRule2SourceEmpty,
		{Name: "source-mismatch"}:        routeRule3SourceMismatch,
		{Name: "source-match"}:           routeRule4SourceMatch,
		{Name: "tag-subset-of-mismatch"}: routeRule5TagSubsetOfMismatch,
		{Name: "tag-subset-of-match"}:    routeRule6TagSubsetOfMatch,
	}
	want := []*proxyconfig.RouteRule{
		routeRule6TagSubsetOfMatch,
		routeRule4SourceMatch,
		routeRule1MatchNil,
		routeRule2SourceEmpty,
	}

	r.mock.EXPECT().List(RouteRule, "").Return(mockObjs, nil)
	got := r.registry.RouteRulesBySource("", instances)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Failed \ngot %+vwant %+v", spew.Sdump(got), spew.Sdump(want))
	}
}

func TestIstioRegistryPoliciesByNamespace(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	cases := []struct {
		name      string
		mockError error
		mockObjs  map[Key]proto.Message
		want      []*proxyconfig.DestinationPolicy
	}{
		{
			name:      "Empty object map with error",
			mockObjs:  map[Key]proto.Message{},
			mockError: errors.New("foobar"),
			want:      []*proxyconfig.DestinationPolicy{},
		},
		{
			name: "Slice of unsorted DestinationPolicy",
			mockObjs: map[Key]proto.Message{
				{Name: "foo"}: dstPolicy1,
				{Name: "bar"}: dstPolicy2,
				{Name: "baz"}: dstPolicy3,
			},
			want: []*proxyconfig.DestinationPolicy{
				dstPolicy1, dstPolicy2, dstPolicy3,
			},
		},
	}
	makeSet := func(in []*proxyconfig.DestinationPolicy) map[*proxyconfig.DestinationPolicy]struct{} {
		out := map[*proxyconfig.DestinationPolicy]struct{}{}
		for _, c := range in {
			out[c] = struct{}{}
		}
		return out
	}

	for _, c := range cases {
		r.mock.EXPECT().List(DestinationPolicy, "").Return(c.mockObjs, c.mockError)
		if got := r.registry.PoliciesByNamespace(""); !reflect.DeepEqual(makeSet(got), makeSet(c.want)) {
			t.Errorf("%v failed: \ngot %+vwant %+v", c.name, spew.Sdump(got), spew.Sdump(c.want))
		}
	}
}

func TestIstioRegistryDestinationPolicies(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	mockObjs := map[Key]proto.Message{
		{Name: "foo"}:  dstPolicy1,
		{Name: "foo2"}: dstPolicy2,
		{Name: "bar"}:  dstPolicy3,
		{Name: "baz"}:  dstPolicy4,
	}
	want := []*proxyconfig.DestinationPolicy{dstPolicy1}

	r.mock.EXPECT().List(DestinationPolicy, "").Return(mockObjs, nil)
	if got := r.registry.DestinationPolicies(dstPolicy1.Destination, dstPolicy1.Tags); !reflect.DeepEqual(got, want) {
		t.Errorf("Failed: \ngot %+vwant %+v", spew.Sdump(got), spew.Sdump(want))
	}
}

func TestKeyString(t *testing.T) {
	// TODO - Tests string formatting with blank name and namespace?
	cases := []struct {
		in   Key
		want string
	}{{
		in:   Key{Kind: "example-kind", Name: "x", Namespace: "default"},
		want: "default/example-kind-x",
	}}
	for _, c := range cases {
		if c.in.String() != c.want {
			t.Errorf("Bad human-readable string: got %v want %v", c.in.String(), c.want)
		}
	}
}

func TestEventString(t *testing.T) {
	cases := []struct {
		in   Event
		want string
	}{
		{EventAdd, "add"},
		{EventUpdate, "update"},
		{EventDelete, "delete"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Failed: got %q want %q", got, c.want)
		}
	}
}

func TestProtoSchemaConversions(t *testing.T) {
	routeRuleSchema := &ProtoSchema{MessageName: RouteRuleProto}

	msg := &proxyconfig.RouteRule{
		Destination: "foo",
		Precedence:  5,
		Route: []*proxyconfig.DestinationWeight{
			{Destination: "bar", Weight: 75},
			{Destination: "baz", Weight: 25},
		},
	}

	wantYAML := "destination: foo\n" +
		"precedence: 5\n" +
		"route:\n" +
		"- destination: bar\n" +
		"  weight: 75\n" +
		"- destination: baz\n" +
		"  weight: 25\n"

	wantJSONMap := map[string]interface{}{
		"destination": "foo",
		"precedence":  5.0,
		"route": []interface{}{
			map[string]interface{}{
				"destination": "bar",
				"weight":      75.0,
			},
			map[string]interface{}{
				"destination": "baz",
				"weight":      25.0,
			},
		},
	}

	badSchema := &ProtoSchema{MessageName: "bad-name"}
	if _, err := badSchema.FromYAML(wantYAML); err == nil {
		t.Errorf("FromYAML should have failed using ProtoSchema with bad MessageName")
	}

	gotYAML, err := routeRuleSchema.ToYAML(msg)
	if err != nil {
		t.Errorf("ToYAML failed: %v", err)
	}
	if !reflect.DeepEqual(gotYAML, wantYAML) {
		t.Errorf("ToYAML failed: got %+v want %+v", spew.Sdump(gotYAML), spew.Sdump(wantYAML))
	}
	gotFromYAML, err := routeRuleSchema.FromYAML(wantYAML)
	if err != nil {
		t.Errorf("FromYAML failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromYAML, msg) {
		t.Errorf("FromYAML failed: got %+v want %+v", spew.Sdump(gotFromYAML), spew.Sdump(msg))
	}

	gotJSONMap, err := routeRuleSchema.ToJSONMap(msg)
	if err != nil {
		t.Errorf("ToJSONMap failed: %v", err)
	}
	if !reflect.DeepEqual(gotJSONMap, wantJSONMap) {
		t.Errorf("ToJSONMap failed: \ngot %vwant %v", spew.Sdump(gotJSONMap), spew.Sdump(wantJSONMap))
	}
	gotFromJSONMap, err := routeRuleSchema.FromJSONMap(wantJSONMap)
	if err != nil {
		t.Errorf("FromJSONMap failed: %v", err)
	}
	if !reflect.DeepEqual(gotFromJSONMap, msg) {
		t.Errorf("FromJSONMap failed: got %+v want %+v", spew.Sdump(gotFromJSONMap), spew.Sdump(msg))
	}
}

func TestPortList(t *testing.T) {
	pl := PortList{
		{Name: "http", Port: 80, Protocol: ProtocolHTTP},
		{Name: "http-alt", Port: 8080, Protocol: ProtocolHTTP},
	}

	gotNames := pl.GetNames()
	wantNames := []string{"http", "http-alt"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("GetNames() failed: got %v want %v", gotNames, wantNames)
	}

	cases := []struct {
		name  string
		port  *Port
		found bool
	}{
		{name: pl[0].Name, port: pl[0], found: true},
		{name: "foobar", found: false},
	}

	for _, c := range cases {
		gotPort, gotFound := pl.Get(c.name)
		if c.found != gotFound || !reflect.DeepEqual(gotPort, c.port) {
			t.Errorf("Get() failed: gotFound=%v wantFound=%v\ngot %+vwant %+v",
				gotFound, c.found, spew.Sdump(gotPort), spew.Sdump(c.port))
		}
	}
}

func TestServiceKey(t *testing.T) {
	svc := &Service{Hostname: "hostname"}

	// Verify Service.Key() delegates to ServiceKey()
	{
		want := "hostname:http:a=b,c=d"
		port := &Port{Name: "http", Port: 80, Protocol: ProtocolHTTP}
		tags := Tags{"a": "b", "c": "d"}
		got := svc.Key(port, tags)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Service.Key() failed: got %v want %v", got, want)
		}
	}

	cases := []struct {
		port PortList
		tags TagsList
		want string
	}{
		{
			port: PortList{
				{Name: "http", Port: 80, Protocol: ProtocolHTTP},
				{Name: "http-alt", Port: 8080, Protocol: ProtocolHTTP},
			},
			tags: TagsList{{"a": "b", "c": "d"}},
			want: "hostname:http,http-alt:a=b,c=d",
		},
		{
			port: PortList{{Name: "http", Port: 80, Protocol: ProtocolHTTP}},
			tags: TagsList{{"a": "b", "c": "d"}},
			want: "hostname:http:a=b,c=d",
		},
		{
			port: PortList{{Port: 80, Protocol: ProtocolHTTP}},
			tags: TagsList{{"a": "b", "c": "d"}},
			want: "hostname::a=b,c=d",
		},
		{
			port: PortList{},
			tags: TagsList{{"a": "b", "c": "d"}},
			want: "hostname::a=b,c=d",
		},
		{
			port: PortList{{Name: "http", Port: 80, Protocol: ProtocolHTTP}},
			tags: TagsList{nil},
			want: "hostname:http",
		},
		{
			port: PortList{{Name: "http", Port: 80, Protocol: ProtocolHTTP}},
			tags: TagsList{},
			want: "hostname:http",
		},
		{
			port: PortList{},
			tags: TagsList{},
			want: "hostname",
		},
	}

	for _, c := range cases {
		got := ServiceKey(svc.Hostname, c.port, c.tags)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Failed: got %q want %q", got, c.want)
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
			name: "empty tags",
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

func TestTagsEquals(t *testing.T) {
	cases := []struct {
		a, b Tags
		want bool
	}{
		{
			a: nil,
			b: Tags{"a": "b"},
		},
		{
			a: Tags{"a": "b"},
			b: nil,
		},
		{
			a:    Tags{"a": "b"},
			b:    Tags{"a": "b"},
			want: true,
		},
	}
	for _, c := range cases {
		if got := c.a.Equals(c.b); got != c.want {
			t.Errorf("Failed: got eq=%v want=%v for %q ?= %q", got, c.want, c.a, c.b)
		}
	}
}

func TestValidateRouteAndIngressRule(t *testing.T) {
	cases := []struct {
		in    proto.Message
		valid bool
	}{
		{in: &proxyconfig.DestinationPolicy{}, valid: false},
		{in: &proxyconfig.RouteRule{}, valid: false},
		{in: &proxyconfig.RouteRule{Destination: "foobar"}, valid: true},
	}
	for _, c := range cases {
		if got := ValidateRouteRule(c.in); (got == nil) != c.valid {
			t.Errorf("ValidateRouteRule failed: got valid=%v but wanted valid=%v: %v", got == nil, c.valid, got)
		}
		if got := ValidateIngressRule(c.in); (got == nil) != c.valid {
			t.Errorf("ValidateIngressRule failed: got valid=%v but wanted valid=%v: %v", got == nil, c.valid, got)
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
	}
	for _, c := range cases {
		if got := ValidateDestinationPolicy(c.in); (got == nil) != c.valid {
			t.Errorf("Failed: got valid=%v but wanted valid=%v: %v", got == nil, c.valid, got)
		}
	}
}
