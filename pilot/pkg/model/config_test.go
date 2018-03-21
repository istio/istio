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
	"errors"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/protobuf/proto"

	authn "istio.io/api/authentication/v1alpha2"
	networking "istio.io/api/networking/v1alpha3"
	routing "istio.io/api/routing/v1alpha1"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1/mock"
	mock_config "istio.io/istio/pilot/test/mock"
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

func TestSubsetKey(t *testing.T) {
	hostname := "hostname"
	cases := []struct {
		hostname string
		subset   string
		port     *model.Port
		want     string
	}{
		{
			hostname: "hostname",
			subset:   "subset",
			port:     &model.Port{Name: "http", Port: 80, Protocol: model.ProtocolHTTP},
			want:     "outbound|http|subset|hostname",
		},
		{
			hostname: "hostname",
			subset:   "subset",
			port:     &model.Port{Port: 80, Protocol: model.ProtocolHTTP},
			want:     "outbound||subset|hostname",
		},
		{
			hostname: "hostname",
			subset:   "",
			port:     &model.Port{Name: "http", Port: 80, Protocol: model.ProtocolHTTP},
			want:     "outbound|http||hostname",
		},
	}

	for _, c := range cases {
		got := model.BuildSubsetKey(model.TrafficDirectionOutbound, c.subset, hostname, c.port)
		if got != c.want {
			t.Errorf("Failed: got %q want %q", got, c.want)
		}

		// test parse subset key. ParseSubsetKey is the inverse of BuildSubsetKey
		_, s, h, p := model.ParseSubsetKey(got)
		if s != c.subset || h != c.hostname || p.Name != c.port.Name {
			t.Errorf("Failed: got %s,%s,%s want %s,%s,%s", s, h, p.Name, c.subset, c.hostname, c.port.Name)
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
	config := mock_config.Make("ns", 2)
	want := "mock-config/ns/mock-config2"
	if key := config.ConfigMeta.Key(); key != want {
		t.Errorf("config.Key() => got %q, want %q", key, want)
	}
}

func TestResolveHostname(t *testing.T) {
	cases := []struct {
		meta model.ConfigMeta
		svc  *routing.IstioService
		want string
	}{
		{
			meta: model.ConfigMeta{Namespace: "default", Domain: "cluster.local"},
			svc:  &routing.IstioService{Name: "hello"},
			want: "hello.default.svc.cluster.local",
		},
		{
			meta: model.ConfigMeta{Namespace: "foo", Domain: "foo"},
			svc: &routing.IstioService{Name: "hello",
				Namespace: "default", Domain: "svc.cluster.local"},
			want: "hello.default.svc.cluster.local",
		},
		{
			meta: model.ConfigMeta{},
			svc:  &routing.IstioService{Name: "hello"},
			want: "hello",
		},
		{
			meta: model.ConfigMeta{Namespace: "default"},
			svc:  &routing.IstioService{Name: "hello"},
			want: "hello.default",
		},
		{
			meta: model.ConfigMeta{Namespace: "default", Domain: "cluster.local"},
			svc:  &routing.IstioService{Service: "reviews.service.consul"},
			want: "reviews.service.consul",
		},
		{
			meta: model.ConfigMeta{Namespace: "foo", Domain: "foo"},
			svc: &routing.IstioService{Name: "hello", Service: "reviews.service.consul",
				Namespace: "default", Domain: "svc.cluster.local"},
			want: "reviews.service.consul",
		},
		{
			meta: model.ConfigMeta{Namespace: "default", Domain: "cluster.local"},
			svc:  &routing.IstioService{Service: "*cnn.com"},
			want: "*cnn.com",
		},
		{
			meta: model.ConfigMeta{Namespace: "foo", Domain: "foo"},
			svc: &routing.IstioService{Name: "hello", Service: "*cnn.com",
				Namespace: "default", Domain: "svc.cluster.local"},
			want: "*cnn.com",
		},
	}

	for _, test := range cases {
		if got := model.ResolveHostname(test.meta, test.svc); got != test.want {
			t.Errorf("ResolveHostname(%v, %v) => got %q, want %q", test.meta, test.svc, got, test.want)
		}
	}
}

func TestResolveFQDN(t *testing.T) {
	cases := []struct {
		name   string
		domain string
		fqdn   string
	}{
		{
			name:   "hello",
			domain: "world.svc.cluster.local",
			fqdn:   "hello.world.svc.cluster.local",
		},
		{
			name:   "hello",
			domain: "",
			fqdn:   "hello",
		},
		{
			name:   "hello.world.svc.cluster.local",
			domain: "world.svc.cluster.local",
			fqdn:   "hello.world.svc.cluster.local",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := model.ResolveFQDN(test.name, test.domain); got != test.fqdn {
				t.Errorf("got %q, want %q", got, test.fqdn)
			}
		})
	}
}

func TestMatchSource(t *testing.T) {
	cases := []struct {
		meta      model.ConfigMeta
		svc       *routing.IstioService
		instances []*model.ServiceInstance
		want      bool
	}{
		{
			meta: model.ConfigMeta{Name: "test", Namespace: "default", Domain: "cluster.local"},
			want: true,
		},
		{
			meta: model.ConfigMeta{Name: "test", Namespace: "default", Domain: "cluster.local"},
			svc:  &routing.IstioService{Name: "hello"},
			want: false,
		},
		{
			meta:      model.ConfigMeta{Name: "test", Namespace: "default", Domain: "cluster.local"},
			svc:       &routing.IstioService{Name: "world"},
			instances: []*model.ServiceInstance{mock.MakeInstance(mock.HelloService, mock.GetPortHTTP(mock.HelloService), 0, "")},
			want:      false,
		},
		{
			meta:      model.ConfigMeta{Name: "test", Namespace: "default", Domain: "cluster.local"},
			svc:       &routing.IstioService{Name: "hello"},
			instances: []*model.ServiceInstance{mock.MakeInstance(mock.HelloService, mock.GetPortHTTP(mock.HelloService), 0, "")},
			want:      true,
		},
		{
			meta:      model.ConfigMeta{Name: "test", Namespace: "default", Domain: "cluster.local"},
			svc:       &routing.IstioService{Name: "hello", Labels: map[string]string{"version": "v0"}},
			instances: []*model.ServiceInstance{mock.MakeInstance(mock.HelloService, mock.GetPortHTTP(mock.HelloService), 0, "")},
			want:      true,
		},
	}

	for _, test := range cases {
		if got := model.MatchSource(test.meta, test.svc, test.instances); got != test.want {
			t.Errorf("MatchSource(%v) => got %v, want %v", test, got, test.want)
		}
	}
}

func TestSortRouteRules(t *testing.T) {
	rules := []model.Config{
		{
			ConfigMeta: model.ConfigMeta{Name: "d"},
			Spec:       &routing.RouteRule{Precedence: 2},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "b"},
			Spec:       &routing.RouteRule{Precedence: 3},
		},
		{
			ConfigMeta: model.ConfigMeta{Name: "c"},
			Spec:       &routing.RouteRule{Precedence: 2},
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

type errorStore struct{}

func (errorStore) ConfigDescriptor() model.ConfigDescriptor {
	return model.IstioConfigTypes
}

func (errorStore) Get(typ, name, namespace string) (*model.Config, bool) {
	return nil, false
}

func (errorStore) List(typ, namespace string) ([]model.Config, error) {
	return nil, errors.New("fail")
}

func (errorStore) Create(config model.Config) (string, error) {
	return "", errors.New("fail more")
}

func (errorStore) Update(config model.Config) (string, error) {
	return "", errors.New("yes, fail again")
}

func (errorStore) Delete(typ, name, namespace string) error {
	return errors.New("just keep failing")
}

func TestRouteRules(t *testing.T) {
	instance := mock.MakeInstance(mock.HelloService, mock.GetPortHTTP(mock.HelloService), 0, "")

	testCases := []struct {
		configType string
		spec       proto.Message
	}{
		{
			configType: model.RouteRule.Type,
			spec: &routing.RouteRule{
				Match: &routing.MatchCondition{
					Source: &routing.IstioService{
						Name:   "hello",
						Labels: instance.Labels,
					},
				},
				Destination: &routing.IstioService{
					Name: "world",
				},
			},
		},
		{
			configType: model.VirtualService.Type,
			spec: &networking.VirtualService{
				Hosts: []string{"world"},
				Http: []*networking.HTTPRoute{
					{
						Route: []*networking.DestinationWeight{{
							Destination: &networking.Destination{
								Name: "world",
							},
						}},
						Match: []*networking.HTTPMatchRequest{
							{
								SourceLabels: instance.Labels,
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.configType, func(t *testing.T) {
			store := model.MakeIstioStore(memory.Make(model.IstioConfigTypes))
			config := model.Config{
				ConfigMeta: model.ConfigMeta{
					Type:      tc.configType,
					Name:      "example",
					Namespace: "default",
					Domain:    "cluster.local",
				},
				Spec: tc.spec,
			}

			if _, err := store.Create(config); err != nil {
				t.Error(err)
			}
			if out := store.RouteRules([]*model.ServiceInstance{instance}, mock.WorldService.Hostname, mock.HelloProxyV0.Domain); len(out) != 1 ||
				!reflect.DeepEqual(tc.spec, out[0].Spec) {
				t.Errorf("RouteRules() => expected %#v but got %#v", tc.spec, out)
			}
			if out := store.RouteRules([]*model.ServiceInstance{instance}, mock.HelloService.Hostname, mock.HelloProxyV0.Domain); len(out) != 0 {
				t.Error("RouteRules() => expected no match for destination-matched rules")
			}
			if out := store.RouteRules(nil, mock.WorldService.Hostname, "DNE"); len(out) != 0 {
				t.Error("RouteRules() => expected no match for source-matched rules")
			}

			world := mock.MakeInstance(mock.WorldService, mock.GetPortHTTP(mock.WorldService), 0, "")
			if out := store.RouteRulesByDestination([]*model.ServiceInstance{world}, mock.HelloProxyV0.Domain); len(out) != 1 ||
				!reflect.DeepEqual(tc.spec, out[0].Spec) {
				t.Errorf("RouteRulesByDestination() => got %#v, want %#v", out, tc.spec)
			}
			if out := store.RouteRulesByDestination([]*model.ServiceInstance{instance}, mock.HelloProxyV0.Domain); len(out) != 0 {
				t.Error("RouteRulesByDestination() => expected no match")
			}

			// erroring out list
			if out := model.MakeIstioStore(errorStore{}).RouteRules([]*model.ServiceInstance{instance},
				mock.WorldService.Hostname, mock.HelloProxyV0.Domain); len(out) != 0 {
				t.Errorf("RouteRules() => expected nil but got %v", out)
			}
			if out := model.MakeIstioStore(errorStore{}).RouteRulesByDestination([]*model.ServiceInstance{world}, mock.HelloProxyV0.Domain); len(out) != 0 {
				t.Errorf("RouteRulesByDestination() => expected nil but got %v", out)
			}
		})
	}
}

func TestEgressRules(t *testing.T) {
	store := model.MakeIstioStore(memory.Make(model.IstioConfigTypes))
	rule := &routing.EgressRule{
		Destination: &routing.IstioService{
			Service: "*.foo.com",
		},
		Ports: []*routing.EgressRule_Port{{
			Port:     80,
			Protocol: "HTTP",
		}},
	}

	config := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.EgressRule.Type,
			Name:      "example",
			Namespace: "default",
			Domain:    "cluster.local",
		},
		Spec: rule,
	}

	if _, err := store.Create(config); err != nil {
		t.Error(err)
	}

	got := store.EgressRules()
	if len(got) != 1 {
		t.Fatalf("EgressRules() => want 1 rule, got %d", len(got))
	}

	gotRule := got[0].Spec

	if !reflect.DeepEqual(gotRule, rule) {
		t.Errorf("EgressRules() => expected \n%#v, got \n%#v", rule, gotRule)
	}

	// erroring out list
	if out := model.MakeIstioStore(errorStore{}).EgressRules(); len(out) != 0 {
		t.Errorf("EgressRules() => expected nil but got %v", out)
	}
}

func TestDestinationPolicy(t *testing.T) {
	store := model.MakeIstioStore(memory.Make(model.IstioConfigTypes))
	labels := map[string]string{"version": "v1"}
	instances := []*model.ServiceInstance{mock.MakeInstance(mock.HelloService, mock.GetPortHTTP(mock.HelloService), 0, "")}

	policy1 := &routing.DestinationPolicy{
		Source: &routing.IstioService{
			Name:   "hello",
			Labels: map[string]string{"version": "v0"},
		},
		Destination: &routing.IstioService{
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

	// erroring out list
	if out := model.MakeIstioStore(errorStore{}).Policy(instances, mock.WorldService.Hostname, labels); out != nil {
		t.Errorf("Policy() => expected nil but got %v", out)
	}
}

func TestAuthenticationPolicyConfig(t *testing.T) {
	store := model.MakeIstioStore(memory.Make(model.IstioConfigTypes))

	authNPolicies := map[string]*authn.Policy{
		"all": {},
		"hello": {
			Targets: []*authn.TargetSelector{{
				Name: "hello",
			}},
			Peers: []*authn.PeerAuthenticationMethod{{
				Params: &authn.PeerAuthenticationMethod_Mtls{},
			}},
		},
		"world": {
			Targets: []*authn.TargetSelector{{
				Name: "world",
				Ports: []*authn.PortSelector{
					{
						Port: &authn.PortSelector_Number{
							Number: 80,
						},
					},
				},
			}},
			Origins: []*authn.OriginAuthenticationMethod{
				{
					Jwt: &authn.Jwt{
						Issuer:  "abc.xzy",
						JwksUri: "https://secure.isio.io",
					},
				},
			},
			PrincipalBinding: authn.PrincipalBinding_USE_ORIGIN,
		},
	}
	for key, value := range authNPolicies {
		config := model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      model.AuthenticationPolicy.Type,
				Name:      key,
				Group:     "authentication",
				Version:   "v1alpha2",
				Namespace: "default",
				Domain:    "cluster.local",
			},
			Spec: value,
		}
		if _, err := store.Create(config); err != nil {
			t.Error(err)
		}
	}

	cases := []struct {
		hostname string
		port     int
		expected string
	}{
		{
			hostname: "hello.default.svc.cluster.local",
			port:     80,
			expected: "hello",
		},
		{
			hostname: "world.default.svc.cluster.local",
			port:     80,
			expected: "world",
		},
		{
			hostname: "world.default.svc.cluster.local",
			port:     8080,
			expected: "all",
		},
		{
			hostname: "world.another-galaxy.svc.cluster.local",
			port:     8080,
			expected: "",
		},
	}

	for _, testCase := range cases {
		port := &model.Port{Port: testCase.port}
		expected := authNPolicies[testCase.expected]
		out := store.AuthenticationPolicyByDestination(testCase.hostname, port)
		if out == nil {
			if expected != nil {
				t.Errorf("AutheticationPolicy(%s:%d) => expected %#v but got nil",
					testCase.hostname, testCase.port, expected)
			}
		} else {
			policy := out.Spec.(*authn.Policy)
			if !reflect.DeepEqual(expected, policy) {
				t.Errorf("AutheticationPolicy(%s:%d) => expected %#v but got %#v",
					testCase.hostname, testCase.port, expected, out)
			}
		}
	}
}
