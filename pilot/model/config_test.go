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
	defaultNamespace = "default"

	serviceInstance1 = &ServiceInstance{
		Endpoint: NetworkEndpoint{
			Address:     "192.168.1.1",
			Port:        10001,
			ServicePort: &Port{Name: "http", Port: 81, Protocol: ProtocolHTTP},
		},
		Service: &Service{
			Hostname: "one.service.com",
			Address:  "192.168.3.1", // VIP
			Ports: PortList{
				&Port{Name: "http", Port: 81, Protocol: ProtocolHTTP},
				&Port{Name: "http-alt", Port: 8081, Protocol: ProtocolHTTP},
			},
		},
		Tags: Tags{"a": "b", "c": "d"},
	}
	serviceInstance2 = &ServiceInstance{
		Endpoint: NetworkEndpoint{
			Address:     "192.168.1.2",
			Port:        10002,
			ServicePort: &Port{Name: "http", Port: 82, Protocol: ProtocolHTTP},
		},
		Service: &Service{
			Hostname: "two.service.com",
			Address:  "192.168.3.2", // VIP
			Ports: PortList{
				&Port{Name: "http", Port: 82, Protocol: ProtocolHTTP},
				&Port{Name: "http-alt", Port: 8282, Protocol: ProtocolHTTP},
			},
		},
		Tags: Tags{"e": "f", "g": "h"},
	}

	routeRule1MatchNil = &proxyconfig.RouteRule{
		Destination: "foo",
		Precedence:  1,
	}

	routeRule2SourceEmpty = &proxyconfig.RouteRule{
		Destination: "foo",
		Precedence:  2,
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
		want      []*proxyconfig.RouteRule
	}{
		{
			name:      "Empty object map with error",
			mockObjs:  map[Key]proto.Message{},
			mockError: errors.New("foobar"),
			want:      []*proxyconfig.RouteRule{},
		},
		{
			name: "Slice of unsorted RouteRules",
			mockObjs: map[Key]proto.Message{
				Key{Kind: "foo"}: routeRule1MatchNil,
				Key{Kind: "bar"}: routeRule3SourceMismatch,
				Key{Kind: "baz"}: routeRule2SourceEmpty,
			},
			want: []*proxyconfig.RouteRule{
				routeRule1MatchNil,
				routeRule3SourceMismatch,
				routeRule2SourceEmpty,
			},
		},
	}
	makeSet := func(in []*proxyconfig.RouteRule) map[*proxyconfig.RouteRule]struct{} {
		out := map[*proxyconfig.RouteRule]struct{}{}
		for _, c := range in {
			out[c] = struct{}{}
		}
		return out
	}

	for _, c := range cases {
		// Use sets to compare unsorted route rules.

		r.mock.EXPECT().List(RouteRule, defaultNamespace).Return(c.mockObjs, c.mockError)
		if got := r.registry.RouteRules(defaultNamespace); !reflect.DeepEqual(makeSet(got), makeSet(c.want)) {
			t.Errorf("%v with RouteRule failed: \ngot %+vwant %+v", c.name, spew.Sdump(got), spew.Sdump(c.want))
		}

		r.mock.EXPECT().List(IngressRule, defaultNamespace).Return(c.mockObjs, c.mockError)
		if got := r.registry.IngressRules(defaultNamespace); !reflect.DeepEqual(makeSet(got), makeSet(c.want)) {
			t.Errorf("%v with IngressRule failed: \ngot %+vwant %+v", c.name, spew.Sdump(got), spew.Sdump(c.want))
		}
	}
}

func TestIstioRegistryRouteRulesBySource(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	instances := []*ServiceInstance{serviceInstance1, serviceInstance2}

	mockObjs := map[Key]proto.Message{
		Key{Kind: "match-nil"}:              routeRule1MatchNil,
		Key{Kind: "source-empty"}:           routeRule2SourceEmpty,
		Key{Kind: "source-mismatch"}:        routeRule3SourceMismatch,
		Key{Kind: "source-match"}:           routeRule4SourceMatch,
		Key{Kind: "tag-subset-of-mismatch"}: routeRule5TagSubsetOfMismatch,
		Key{Kind: "tag-subset-of-match"}:    routeRule6TagSubsetOfMatch,
	}
	want := []*proxyconfig.RouteRule{
		routeRule6TagSubsetOfMatch,
		routeRule4SourceMatch,
		routeRule2SourceEmpty,
		routeRule1MatchNil,
	}

	r.mock.EXPECT().List(RouteRule, defaultNamespace).Return(mockObjs, nil)
	got := r.registry.RouteRulesBySource(defaultNamespace, instances)
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
				Key{Kind: "foo"}: dstPolicy1,
				Key{Kind: "bar"}: dstPolicy2,
				Key{Kind: "baz"}: dstPolicy3,
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
		r.mock.EXPECT().List(DestinationPolicy, defaultNamespace).Return(c.mockObjs, c.mockError)
		if got := r.registry.PoliciesByNamespace(defaultNamespace); !reflect.DeepEqual(makeSet(got), makeSet(c.want)) {
			t.Errorf("%v failed: \ngot %+vwant %+v", c.name, spew.Sdump(got), spew.Sdump(c.want))
		}
	}
}

func TestIstioRegistryDestinationPolicies(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	mockObjs := map[Key]proto.Message{
		Key{Kind: "foo"}:  dstPolicy1,
		Key{Kind: "foo2"}: dstPolicy2,
		Key{Kind: "bar"}:  dstPolicy3,
		Key{Kind: "baz"}:  dstPolicy4,
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
		in:   Key{Kind: "ExampleKind", Name: "x", Namespace: "default"},
		want: "default/ExampleKind-x",
	}}
	for _, c := range cases {
		if c.in.String() != c.want {
			t.Errorf("Bad human-readable string: got %v want %v", c.in.String(), c.want)
		}
	}
}
