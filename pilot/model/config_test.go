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
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/mock/gomock"

	proxyconfig "istio.io/api/proxy/v1/config"
)

func TestConfigDescriptor(t *testing.T) {
	a := ProtoSchema{Type: "a", MessageName: "proxy.A"}
	descriptor := ConfigDescriptor{
		a,
		ProtoSchema{Type: "b", MessageName: "proxy.B"},
		ProtoSchema{Type: "c", MessageName: "proxy.C"},
	}
	want := []string{"a", "b", "c"}
	got := descriptor.Types()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("descriptor.Types() => got %+vwant %+v", spew.Sdump(got), spew.Sdump(want))
	}

	aType, aExists := descriptor.GetByType(a.Type)
	if !aExists || !reflect.DeepEqual(aType, a) {
		t.Errorf("descriptor.GetByType(a) => got %+v, want %+v", aType, a)
	}

	aSchema, aSchemaExists := descriptor.GetByMessageName(a.MessageName)
	if !aSchemaExists || !reflect.DeepEqual(aSchema, a) {
		t.Errorf("descriptor.GetByMessageName(a) => got %+v, want %+v", aType, a)
	}
	_, aSchemaNotExist := descriptor.GetByMessageName("blah")
	if aSchemaNotExist {
		t.Errorf("descriptor.GetByMessageName(blah) => got true, want false")
	}
}

type testRegistry struct {
	ctrl     gomock.Controller
	mock     *MockConfigStore
	registry IstioConfigStore
}

func initTestRegistry(t *testing.T) *testRegistry {
	ctrl := gomock.NewController(t)
	mock := NewMockConfigStore(ctrl)
	return &testRegistry{
		mock:     mock,
		registry: MakeIstioStore(mock),
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
		Name:        "foo",
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

	dstPolicy1 = &proxyconfig.DestinationPolicy{
		Destination: "foo",
		Policy:      []*proxyconfig.DestinationVersionPolicy{{Tags: dstTags0}},
	}
	dstPolicy2 = &proxyconfig.DestinationPolicy{
		Destination: "bar",
	}
	dstPolicy3 = &proxyconfig.DestinationPolicy{
		Destination: "baz",
		Policy:      []*proxyconfig.DestinationVersionPolicy{{Tags: dstTags1}},
	}
)

func TestIstioRegistryRouteRules(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	cases := []struct {
		name      string
		mockError error
		mockObjs  []Config
		want      map[string]*proxyconfig.RouteRule
	}{
		{
			name:      "Empty object map with error",
			mockObjs:  nil,
			mockError: errors.New("foobar"),
			want:      map[string]*proxyconfig.RouteRule{},
		},
		{
			name: "Slice of unsorted RouteRules",
			mockObjs: []Config{
				{ConfigMeta: ConfigMeta{Name: "foo"}, Spec: routeRule1MatchNil},
				{ConfigMeta: ConfigMeta{Name: "bar"}, Spec: routeRule3SourceMismatch},
				{ConfigMeta: ConfigMeta{Name: "baz"}, Spec: routeRule2SourceEmpty},
			},
			want: map[string]*proxyconfig.RouteRule{
				"//foo": routeRule1MatchNil,
				"//bar": routeRule3SourceMismatch,
				"//baz": routeRule2SourceEmpty,
			},
		},
	}
	for _, c := range cases {
		r.mock.EXPECT().List(RouteRule.Type, "").Return(c.mockObjs, c.mockError)
		if got := r.registry.RouteRules(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v with RouteRule failed: \ngot %+vwant %+v", c.name, spew.Sdump(got), spew.Sdump(c.want))
		}
	}
}

func TestIstioRegistryIngressRules(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	rule := &proxyconfig.IngressRule{
		Name:        "sample-ingress",
		Destination: "a.svc",
	}

	r.mock.EXPECT().List(IngressRule.Type, "").Return([]Config{{
		ConfigMeta: ConfigMeta{
			Name: rule.Name,
		},
		Spec: rule,
	}}, nil)

	if got := r.registry.IngressRules(); !reflect.DeepEqual(got, map[string]*proxyconfig.IngressRule{
		"//" + rule.Name: rule,
	}) {
		t.Errorf("IngressRules failed: \ngot %+vwant %+v", spew.Sdump(got), spew.Sdump(rule))
	}

	r.mock.EXPECT().List(IngressRule.Type, "").Return(nil, errors.New("cannot list"))
	if got := r.registry.IngressRules(); len(got) > 0 {
		t.Errorf("IngressRules failed: \ngot %+vwant empty", spew.Sdump(got))
	}

}

func TestIstioRegistryRouteRulesBySource(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	instances := []*ServiceInstance{serviceInstance1, serviceInstance2}

	mockObjs := []Config{
		{ConfigMeta: ConfigMeta{Name: "match-nil"}, Spec: routeRule1MatchNil},
		{ConfigMeta: ConfigMeta{Name: "source-empty"}, Spec: routeRule2SourceEmpty},
		{ConfigMeta: ConfigMeta{Name: "source-mismatch"}, Spec: routeRule3SourceMismatch},
		{ConfigMeta: ConfigMeta{Name: "source-match"}, Spec: routeRule4SourceMatch},
		{ConfigMeta: ConfigMeta{Name: "tag-subset-of-mismatch"}, Spec: routeRule5TagSubsetOfMismatch},
		{ConfigMeta: ConfigMeta{Name: "tag-subset-of-match"}, Spec: routeRule6TagSubsetOfMatch},
	}
	want := []*proxyconfig.RouteRule{
		routeRule6TagSubsetOfMatch,
		routeRule4SourceMatch,
		routeRule1MatchNil,
		routeRule2SourceEmpty,
	}

	r.mock.EXPECT().List(RouteRule.Type, "").Return(mockObjs, nil)
	got := r.registry.RouteRulesBySource(instances)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Failed \ngot %+vwant %+v", spew.Sdump(got), spew.Sdump(want))
	}
}

func TestIstioRegistryPolicies(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	cases := []struct {
		name      string
		mockError error
		mockObjs  []Config
		want      []*proxyconfig.DestinationPolicy
	}{
		{
			name:      "Empty object map with error",
			mockObjs:  nil,
			mockError: errors.New("foobar"),
			want:      []*proxyconfig.DestinationPolicy{},
		},
		{
			name: "Slice of unsorted DestinationPolicy",
			mockObjs: []Config{
				{ConfigMeta: ConfigMeta{Name: "foo"}, Spec: dstPolicy1},
				{ConfigMeta: ConfigMeta{Name: "bar"}, Spec: dstPolicy2},
				{ConfigMeta: ConfigMeta{Name: "baz"}, Spec: dstPolicy3},
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
		r.mock.EXPECT().List(DestinationPolicy.Type, "").Return(c.mockObjs, c.mockError)
		if got := r.registry.DestinationPolicies(); !reflect.DeepEqual(makeSet(got), makeSet(c.want)) {
			t.Errorf("%v failed: \ngot %+vwant %+v", c.name, spew.Sdump(got), spew.Sdump(c.want))
		}
	}
}

func TestIstioRegistryDestinationPolicies(t *testing.T) {
	r := initTestRegistry(t)
	defer r.shutdown()

	r.mock.EXPECT().Get(DestinationPolicy.Type, dstPolicy1.Destination, "").Return(&Config{
		Spec: dstPolicy1,
	}, true)
	want := dstPolicy1.Policy[0]
	if got := r.registry.DestinationPolicy(dstPolicy1.Destination, want.Tags); !reflect.DeepEqual(got, want) {
		t.Errorf("Failed: \ngot %+vwant %+v", spew.Sdump(got), spew.Sdump(want))
	}

	r.mock.EXPECT().Get(DestinationPolicy.Type, dstPolicy3.Destination, "").Return(nil, false)
	if got := r.registry.DestinationPolicy(dstPolicy3.Destination, nil); got != nil {
		t.Errorf("Failed: \ngot %+vwant nil", spew.Sdump(got))
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
	routeRuleSchema := &ProtoSchema{MessageName: RouteRule.MessageName}

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
		want := "hostname|http|a=b,c=d"
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
			want: "hostname|http,http-alt|a=b,c=d",
		},
		{
			port: PortList{{Name: "http", Port: 80, Protocol: ProtocolHTTP}},
			tags: TagsList{{"a": "b", "c": "d"}},
			want: "hostname|http|a=b,c=d",
		},
		{
			port: PortList{{Port: 80, Protocol: ProtocolHTTP}},
			tags: TagsList{{"a": "b", "c": "d"}},
			want: "hostname||a=b,c=d",
		},
		{
			port: PortList{},
			tags: TagsList{{"a": "b", "c": "d"}},
			want: "hostname||a=b,c=d",
		},
		{
			port: PortList{{Name: "http", Port: 80, Protocol: ProtocolHTTP}},
			tags: TagsList{nil},
			want: "hostname|http",
		},
		{
			port: PortList{{Name: "http", Port: 80, Protocol: ProtocolHTTP}},
			tags: TagsList{},
			want: "hostname|http",
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
