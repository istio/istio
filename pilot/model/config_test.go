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

package model_test

import (
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"

	proxyconfig "istio.io/api/proxy/v1/config"
	"istio.io/pilot/adapter/config/memory"
	"istio.io/pilot/model"
	"istio.io/pilot/test/mock"
)

func TestConfigDescriptor(t *testing.T) {
	a := model.ProtoSchema{Type: "a", MessageName: "proxy.A"}
	descriptor := model.ConfigDescriptor{
		a,
		model.ProtoSchema{Type: "b", MessageName: "proxy.B"},
		model.ProtoSchema{Type: "c", MessageName: "proxy.C"},
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
	if _, exists := descriptor.GetByType("missing"); exists {
		t.Error("descriptor.GetByType(missing) => got true, want false")
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

func TestEventString(t *testing.T) {
	cases := []struct {
		in   model.Event
		want string
	}{
		{model.EventAdd, "add"},
		{model.EventUpdate, "update"},
		{model.EventDelete, "delete"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Errorf("Failed: got %q want %q", got, c.want)
		}
	}
}

func TestProtoSchemaConversions(t *testing.T) {
	routeRuleSchema := &model.ProtoSchema{MessageName: model.RouteRule.MessageName}

	msg := &proxyconfig.RouteRule{
		Destination: &proxyconfig.IstioService{
			Name: "foo",
		},
		Precedence: 5,
		Route: []*proxyconfig.DestinationWeight{
			{Destination: "bar", Weight: 75},
			{Destination: "baz", Weight: 25},
		},
	}

	wantYAML := "destination:\n" +
		"  name: foo\n" +
		"precedence: 5\n" +
		"route:\n" +
		"- destination: bar\n" +
		"  weight: 75\n" +
		"- destination: baz\n" +
		"  weight: 25\n"

	wantJSONMap := map[string]interface{}{
		"destination": map[string]interface{}{
			"name": "foo",
		},
		"precedence": 5.0,
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

	badSchema := &model.ProtoSchema{MessageName: "bad-name"}
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
	pl := model.PortList{
		{Name: "http", Port: 80, Protocol: model.ProtocolHTTP},
		{Name: "http-alt", Port: 8080, Protocol: model.ProtocolHTTP},
	}

	gotNames := pl.GetNames()
	wantNames := []string{"http", "http-alt"}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("GetNames() failed: got %v want %v", gotNames, wantNames)
	}

	cases := []struct {
		name  string
		port  *model.Port
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
	svc := &model.Service{Hostname: "hostname"}

	// Verify Service.Key() delegates to ServiceKey()
	{
		want := "hostname|http|a=b,c=d"
		port := &model.Port{Name: "http", Port: 80, Protocol: model.ProtocolHTTP}
		labels := model.Labels{"a": "b", "c": "d"}
		got := svc.Key(port, labels)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Service.Key() failed: got %v want %v", got, want)
		}
	}

	cases := []struct {
		port   model.PortList
		labels model.LabelsCollection
		want   string
	}{
		{
			port: model.PortList{
				{Name: "http", Port: 80, Protocol: model.ProtocolHTTP},
				{Name: "http-alt", Port: 8080, Protocol: model.ProtocolHTTP},
			},
			labels: model.LabelsCollection{{"a": "b", "c": "d"}},
			want:   "hostname|http,http-alt|a=b,c=d",
		},
		{
			port:   model.PortList{{Name: "http", Port: 80, Protocol: model.ProtocolHTTP}},
			labels: model.LabelsCollection{{"a": "b", "c": "d"}},
			want:   "hostname|http|a=b,c=d",
		},
		{
			port:   model.PortList{{Port: 80, Protocol: model.ProtocolHTTP}},
			labels: model.LabelsCollection{{"a": "b", "c": "d"}},
			want:   "hostname||a=b,c=d",
		},
		{
			port:   model.PortList{},
			labels: model.LabelsCollection{{"a": "b", "c": "d"}},
			want:   "hostname||a=b,c=d",
		},
		{
			port:   model.PortList{{Name: "http", Port: 80, Protocol: model.ProtocolHTTP}},
			labels: model.LabelsCollection{nil},
			want:   "hostname|http",
		},
		{
			port:   model.PortList{{Name: "http", Port: 80, Protocol: model.ProtocolHTTP}},
			labels: model.LabelsCollection{},
			want:   "hostname|http",
		},
		{
			port:   model.PortList{},
			labels: model.LabelsCollection{},
			want:   "hostname",
		},
	}

	for _, c := range cases {
		got := model.ServiceKey(svc.Hostname, c.port, c.labels)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Failed: got %q want %q", got, c.want)
		}
	}
}

func TestLabelsEquals(t *testing.T) {
	cases := []struct {
		a, b model.Labels
		want bool
	}{
		{
			a: nil,
			b: model.Labels{"a": "b"},
		},
		{
			a: model.Labels{"a": "b"},
			b: nil,
		},
		{
			a:    model.Labels{"a": "b"},
			b:    model.Labels{"a": "b"},
			want: true,
		},
	}
	for _, c := range cases {
		if got := c.a.Equals(c.b); got != c.want {
			t.Errorf("Failed: got eq=%v want=%v for %q ?= %q", got, c.want, c.a, c.b)
		}
	}
}

func TestConfigKey(t *testing.T) {
	config := mock.Make("ns", 2)
	want := "mock-config/ns/mock-config2"
	if key := config.ConfigMeta.Key(); key != want {
		t.Errorf("config.Key() => got %q, want %q", key, want)
	}
}

func TestResolveHostname(t *testing.T) {
	cases := []struct {
		meta model.ConfigMeta
		svc  *proxyconfig.IstioService
		want string
	}{
		{
			meta: model.ConfigMeta{Namespace: "default", Domain: "cluster.local"},
			svc:  &proxyconfig.IstioService{Name: "hello"},
			want: "hello.default.svc.cluster.local",
		},
		{
			meta: model.ConfigMeta{Namespace: "foo", Domain: "foo"},
			svc:  &proxyconfig.IstioService{Name: "hello", Namespace: "default", Domain: "svc.cluster.local"},
			want: "hello.default.svc.cluster.local",
		},
		{
			meta: model.ConfigMeta{},
			svc:  &proxyconfig.IstioService{Name: "hello"},
			want: "hello",
		},
		{
			meta: model.ConfigMeta{Namespace: "default"},
			svc:  &proxyconfig.IstioService{Name: "hello"},
			want: "hello.default",
		},
	}

	for _, test := range cases {
		if got := model.ResolveHostname(test.meta, test.svc); got != test.want {
			t.Errorf("ResolveHostname(%v, %v) => got %q, want %q", test.meta, test.svc, got, test.want)
		}
	}
}

func TestMatchSource(t *testing.T) {
	cases := []struct {
		meta      model.ConfigMeta
		svc       *proxyconfig.IstioService
		instances []*model.ServiceInstance
		want      bool
	}{
		{
			meta: model.ConfigMeta{Name: "test", Namespace: "default", Domain: "cluster.local"},
			want: true,
		},
		{
			meta: model.ConfigMeta{Name: "test", Namespace: "default", Domain: "cluster.local"},
			svc:  &proxyconfig.IstioService{Name: "hello"},
			want: false,
		},
		{
			meta:      model.ConfigMeta{Name: "test", Namespace: "default", Domain: "cluster.local"},
			svc:       &proxyconfig.IstioService{Name: "world"},
			instances: []*model.ServiceInstance{mock.MakeInstance(mock.HelloService, mock.PortHTTP, 0)},
			want:      false,
		},
		{
			meta:      model.ConfigMeta{Name: "test", Namespace: "default", Domain: "cluster.local"},
			svc:       &proxyconfig.IstioService{Name: "hello"},
			instances: []*model.ServiceInstance{mock.MakeInstance(mock.HelloService, mock.PortHTTP, 0)},
			want:      true,
		},
		{
			meta:      model.ConfigMeta{Name: "test", Namespace: "default", Domain: "cluster.local"},
			svc:       &proxyconfig.IstioService{Name: "hello", Labels: map[string]string{"version": "v0"}},
			instances: []*model.ServiceInstance{mock.MakeInstance(mock.HelloService, mock.PortHTTP, 0)},
			want:      true,
		},
	}

	for _, test := range cases {
		if got := model.MatchSource(test.meta, test.svc, test.instances); got != test.want {
			t.Errorf("MatchSource(%v) => got %q, want %q", test, got, test.want)
		}
	}
}

func TestSortRouteRules(t *testing.T) {
	rules := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "d"},
			Spec:       &proxyconfig.RouteRule{Precedence: 2},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "b"},
			Spec:       &proxyconfig.RouteRule{Precedence: 3},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "c"},
			Spec:       &proxyconfig.RouteRule{Precedence: 2},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "a"},
		},
	}
	model.SortRouteRules(rules)
	if !(rules[0].Name == "a" && rules[1].Name == "b" && rules[2].Name == "c" && rules[3].Name == "d") {
		t.Errorf("SortRouteRules() => got %#v, want a, b, c, d", rules)
	}
}

func TestRouteRules(t *testing.T) {
	store := model.MakeIstioStore(memory.Make(model.IstioConfigTypes))
	instance := mock.MakeInstance(mock.HelloService, mock.PortHTTP, 0)

	routerule1 := &proxyconfig.RouteRule{
		Match: &proxyconfig.MatchCondition{
			Source: &proxyconfig.IstioService{
				Name:   "hello",
				Labels: instance.Labels,
			},
		},
		Destination: &proxyconfig.IstioService{
			Name: "world",
		},
	}

	config1 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.RouteRule.Type,
			Name:      "example",
			Namespace: "default",
			Domain:    "cluster.local",
		},
		Spec: routerule1,
	}

	if _, err := store.Create(config1); err != nil {
		t.Error(err)
	}
	if out := store.RouteRules([]*model.ServiceInstance{instance}, mock.WorldService.Hostname); len(out) != 1 ||
		!reflect.DeepEqual(routerule1, out[0].Spec) {
		t.Errorf("RouteRules() => expected %#v but got %#v", routerule1, out)
	}
	if out := store.RouteRules([]*model.ServiceInstance{instance}, mock.HelloService.Hostname); len(out) != 0 {
		t.Error("RouteRules() => expected no match for destination-matched rules")
	}
	if out := store.RouteRules(nil, mock.WorldService.Hostname); len(out) != 0 {
		t.Error("RouteRules() => expected no match for source-matched rules")
	}

	world := mock.MakeInstance(mock.WorldService, mock.PortHTTP, 0)
	if out := store.RouteRulesByDestination([]*model.ServiceInstance{world}); len(out) != 1 ||
		!reflect.DeepEqual(routerule1, out[0].Spec) {
		t.Errorf("RouteRulesByDestination() => got %#v, want %#v", out, routerule1)
	}
	if out := store.RouteRulesByDestination([]*model.ServiceInstance{instance}); len(out) != 0 {
		t.Error("RouteRulesByDestination() => expected no match")
	}
}

func TestPolicy(t *testing.T) {
	store := model.MakeIstioStore(memory.Make(model.IstioConfigTypes))
	labels := map[string]string{"version": "v1"}
	instances := []*model.ServiceInstance{mock.MakeInstance(mock.HelloService, mock.PortHTTP, 0)}

	policy1 := &proxyconfig.DestinationPolicy{
		Source: &proxyconfig.IstioService{
			Name:   "hello",
			Labels: map[string]string{"version": "v0"},
		},
		Destination: &proxyconfig.IstioService{
			Name:   "world",
			Labels: labels,
		},
	}

	config1 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.DestinationPolicy.Type,
			Name:      "example",
			Namespace: "default",
			Domain:    "cluster.local",
		},
		Spec: policy1,
	}

	if _, err := store.Create(config1); err != nil {
		t.Error(err)
	}
	if out := store.Policy(instances, mock.WorldService.Hostname, labels); out == nil ||
		!reflect.DeepEqual(policy1, out.Spec) {
		t.Errorf("Policy() => expected %#v but got %#v", policy1, out)
	}
	if out := store.Policy(instances, mock.HelloService.Hostname, labels); out != nil {
		t.Error("Policy() => expected no match for destination-matched policy")
	}
	if out := store.Policy(instances, mock.WorldService.Hostname, nil); out != nil {
		t.Error("Policy() => expected no match for labels-matched policy")
	}
	if out := store.Policy(nil, mock.WorldService.Hostname, labels); out != nil {
		t.Error("Policy() => expected no match for source-matched policy")
	}
}
