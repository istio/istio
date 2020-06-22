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

package model_test

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"

	mccpb "istio.io/api/mixer/v1/config/client"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	mock_config "istio.io/istio/pilot/test/mock"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/resource"
)

// getByMessageName finds a schema by message name if it is available
// In test setup, we do not have more than one descriptor with the same message type, so this
// function is ok for testing purpose.
func getByMessageName(schemas collection.Schemas, name string) (collection.Schema, bool) {
	for _, s := range schemas.All() {
		if s.Resource().Proto() == name {
			return s, true
		}
	}
	return nil, false
}

func schemaFor(kind, proto string) collection.Schema {
	return collection.Builder{
		Name: kind,
		Resource: resource.Builder{
			Kind:   kind,
			Plural: kind + "s",
			Proto:  proto,
		}.BuildNoValidate(),
	}.MustBuild()
}

func TestConfigDescriptor(t *testing.T) {
	a := schemaFor("a", "proxy.A")
	schemas := collection.SchemasFor(
		a,
		schemaFor("b", "proxy.B"),
		schemaFor("c", "proxy.C"))
	want := []string{"a", "b", "c"}
	got := schemas.Kinds()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("descriptor.Types() => got %+vwant %+v", spew.Sdump(got), spew.Sdump(want))
	}

	aType, aExists := schemas.FindByGroupVersionKind(a.Resource().GroupVersionKind())
	if !aExists || !reflect.DeepEqual(aType, a) {
		t.Errorf("descriptor.GetByType(a) => got %+v, want %+v", aType, a)
	}
	if _, exists := schemas.FindByGroupVersionKind(resource.GroupVersionKind{Kind: "missing"}); exists {
		t.Error("descriptor.GetByType(missing) => got true, want false")
	}

	aSchema, aSchemaExists := getByMessageName(schemas, a.Resource().Proto())
	if !aSchemaExists || !reflect.DeepEqual(aSchema, a) {
		t.Errorf("descriptor.GetByMessageName(a) => got %+v, want %+v", aType, a)
	}
	_, aSchemaNotExist := getByMessageName(schemas, "blah")
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
		{Name: "http", Port: 80, Protocol: protocol.HTTP},
		{Name: "http-alt", Port: 8080, Protocol: protocol.HTTP},
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

func TestSubsetKey(t *testing.T) {
	hostname := host.Name("hostname")
	cases := []struct {
		hostname host.Name
		subset   string
		port     int
		want     string
	}{
		{
			hostname: "hostname",
			subset:   "subset",
			port:     80,
			want:     "outbound|80|subset|hostname",
		},
		{
			hostname: "hostname",
			subset:   "",
			port:     80,
			want:     "outbound|80||hostname",
		},
	}

	for _, c := range cases {
		got := model.BuildSubsetKey(model.TrafficDirectionOutbound, c.subset, hostname, c.port)
		if got != c.want {
			t.Errorf("Failed: got %q want %q", got, c.want)
		}

		// test parse subset key. ParseSubsetKey is the inverse of BuildSubsetKey
		_, s, h, p := model.ParseSubsetKey(got)
		if s != c.subset || h != c.hostname || p != c.port {
			t.Errorf("Failed: got %s,%s,%d want %s,%s,%d", s, h, p, c.subset, c.hostname, c.port)
		}
	}
}

func TestLabelsEquals(t *testing.T) {
	cases := []struct {
		a, b labels.Instance
		want bool
	}{
		{
			a: nil,
			b: labels.Instance{"a": "b"},
		},
		{
			a: labels.Instance{"a": "b"},
			b: nil,
		},
		{
			a:    labels.Instance{"a": "b"},
			b:    labels.Instance{"a": "b"},
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
	cfg := mock_config.Make("ns", 2)
	want := "MockConfig/ns/mock-config2"
	if key := cfg.ConfigMeta.Key(); key != want {
		t.Fatalf("config.Key() => got %q, want %q", key, want)
	}
}

func TestResolveHostname(t *testing.T) {
	cases := []struct {
		meta model.ConfigMeta
		svc  *mccpb.IstioService
		want host.Name
	}{
		{
			meta: model.ConfigMeta{Namespace: "default", Domain: "cluster.local"},
			svc:  &mccpb.IstioService{Name: "hello"},
			want: "hello.default.svc.cluster.local",
		},
		{
			meta: model.ConfigMeta{Namespace: "foo", Domain: "foo"},
			svc: &mccpb.IstioService{Name: "hello",
				Namespace: "default", Domain: "svc.cluster.local"},
			want: "hello.default.svc.cluster.local",
		},
		{
			meta: model.ConfigMeta{},
			svc:  &mccpb.IstioService{Name: "hello"},
			want: "hello",
		},
		{
			meta: model.ConfigMeta{Namespace: "default"},
			svc:  &mccpb.IstioService{Name: "hello"},
			want: "hello.default",
		},
		{
			meta: model.ConfigMeta{Namespace: "default", Domain: "cluster.local"},
			svc:  &mccpb.IstioService{Service: "reviews.service.consul"},
			want: "reviews.service.consul",
		},
		{
			meta: model.ConfigMeta{Namespace: "foo", Domain: "foo"},
			svc: &mccpb.IstioService{Name: "hello", Service: "reviews.service.consul",
				Namespace: "default", Domain: "svc.cluster.local"},
			want: "reviews.service.consul",
		},
		{
			meta: model.ConfigMeta{Namespace: "default", Domain: "cluster.local"},
			svc:  &mccpb.IstioService{Service: "*cnn.com"},
			want: "*cnn.com",
		},
		{
			meta: model.ConfigMeta{Namespace: "foo", Domain: "foo"},
			svc: &mccpb.IstioService{Name: "hello", Service: "*cnn.com",
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

func TestResolveShortnameToFQDN(t *testing.T) {
	tests := []struct {
		name string
		meta model.ConfigMeta
		out  host.Name
	}{
		{
			"*", model.ConfigMeta{}, "*",
		},
		{
			"*", model.ConfigMeta{Namespace: "default", Domain: "cluster.local"}, "*",
		},
		{
			"foo", model.ConfigMeta{Namespace: "default", Domain: "cluster.local"}, "foo.default.svc.cluster.local",
		},
		{
			"foo.bar", model.ConfigMeta{Namespace: "default", Domain: "cluster.local"}, "foo.bar",
		},
		{
			"foo", model.ConfigMeta{Domain: "cluster.local"}, "foo.svc.cluster.local",
		},
		{
			"foo", model.ConfigMeta{Namespace: "default"}, "foo.default",
		},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.out), func(t *testing.T) {
			if actual := model.ResolveShortnameToFQDN(tt.name, tt.meta); actual != tt.out {
				t.Fatalf("model.ResolveShortnameToFQDN(%q, %v) = %q wanted %q", tt.name, tt.meta, actual, tt.out)
			}
		})
	}
}

func TestMostSpecificHostMatch(t *testing.T) {
	tests := []struct {
		in     []host.Name
		needle host.Name
		want   host.Name
	}{
		// this has to be a sorted list
		{[]host.Name{}, "*", ""},
		{[]host.Name{"*.foo.com", "*.com"}, "bar.foo.com", "*.foo.com"},
		{[]host.Name{"*.foo.com", "*.com"}, "foo.com", "*.com"},
		{[]host.Name{"foo.com", "*.com"}, "*.foo.com", "*.com"},

		{[]host.Name{"*.foo.com", "foo.com"}, "foo.com", "foo.com"},
		{[]host.Name{"*.foo.com", "foo.com"}, "*.foo.com", "*.foo.com"},

		// this passes because we sort alphabetically
		{[]host.Name{"bar.com", "foo.com"}, "*.com", ""},

		{[]host.Name{"bar.com", "*.foo.com"}, "*foo.com", ""},
		{[]host.Name{"foo.com", "*.foo.com"}, "*foo.com", ""},

		// should prioritize closest match
		{[]host.Name{"*.bar.com", "foo.bar.com"}, "foo.bar.com", "foo.bar.com"},
		{[]host.Name{"*.foo.bar.com", "bar.foo.bar.com"}, "bar.foo.bar.com", "bar.foo.bar.com"},

		// should not match non-wildcards for wildcard needle
		{[]host.Name{"bar.foo.com", "foo.bar.com"}, "*.foo.com", ""},
		{[]host.Name{"foo.bar.foo.com", "bar.foo.bar.com"}, "*.bar.foo.com", ""},
	}

	for idx, tt := range tests {
		t.Run(fmt.Sprintf("[%d] %s", idx, tt.needle), func(t *testing.T) {
			actual, found := model.MostSpecificHostMatch(tt.needle, tt.in)
			if tt.want != "" && !found {
				t.Fatalf("model.MostSpecificHostMatch(%q, %v) = %v, %t; want: %v", tt.needle, tt.in, actual, found, tt.want)
			} else if actual != tt.want {
				t.Fatalf("model.MostSpecificHostMatch(%q, %v) = %v, %t; want: %v", tt.needle, tt.in, actual, found, tt.want)
			}
		})
	}
}

func TestAuthorizationPolicies(t *testing.T) {
	store := model.MakeIstioStore(memory.Make(collections.Pilot))
	tests := []struct {
		namespace  string
		expectName map[string]bool
	}{
		{namespace: "wrong", expectName: nil},
		{namespace: "default", expectName: map[string]bool{"policy2": true}},
		{namespace: "istio-system", expectName: map[string]bool{"policy1": true, "policy3": true}},
	}

	for _, tt := range tests {
		cfg := store.AuthorizationPolicies(tt.namespace)
		if tt.expectName != nil {
			for _, cfg := range cfg {
				if !tt.expectName[cfg.Name] {
					t.Errorf("model.AuthorizationPolicy: expecting %v, but got %v", tt.expectName, cfg)
				}
			}
		} else if len(cfg) != 0 {
			t.Errorf("model.AuthorizationPolicy: expecting nil, but got %v", cfg)
		}
	}
}

type fakeStore struct {
	model.ConfigStore
	cfg map[resource.GroupVersionKind][]model.Config
	err error
}

func (l *fakeStore) List(typ resource.GroupVersionKind, namespace string) ([]model.Config, error) {
	ret := l.cfg[typ]
	return ret, l.err
}

func (l *fakeStore) Schemas() collection.Schemas {
	return collections.Pilot
}

func TestIstioConfigStore_QuotaSpecByDestination(t *testing.T) {
	ns := "ns1"
	l := &fakeStore{
		cfg: map[resource.GroupVersionKind][]model.Config{
			gvk.QuotaSpecBinding: {
				{
					ConfigMeta: model.ConfigMeta{
						Namespace: ns,
						Domain:    "cluster.local",
					},
					Spec: &mccpb.QuotaSpecBinding{
						Services: []*mccpb.IstioService{
							{
								Name:      "a",
								Namespace: ns,
							},
						},
						QuotaSpecs: []*mccpb.QuotaSpecBinding_QuotaSpecReference{
							{
								Name: "request-count",
							},
							{
								Name: "does-not-exist",
							},
						},
					},
				},
			},
			gvk.QuotaSpec: {
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "request-count",
						Namespace: ns,
					},
					Spec: &mccpb.QuotaSpec{
						Rules: []*mccpb.QuotaRule{
							{
								Quotas: []*mccpb.Quota{
									{
										Quota:  "requestcount",
										Charge: 100,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	ii := model.MakeIstioStore(l)
	cfgs := ii.QuotaSpecByDestination(host.Name("a." + ns + ".svc.cluster.local"))

	if len(cfgs) != 1 {
		t.Fatalf("did not find 1 matched quota")
	}
}

func TestMatchesDestHost(t *testing.T) {
	for _, tst := range []struct {
		destinationHost string
		svc             string
		ans             bool
	}{
		{
			destinationHost: "myhost.ns.cluster.local",
			svc:             "myhost.ns.cluster.local",
			ans:             true,
		},
		{
			destinationHost: "myhost.ns.cluster.local",
			svc:             "*",
			ans:             true,
		},
		{
			destinationHost: "myhost.ns.cluster.local",
			svc:             "*.ns.*",
			ans:             true,
		},
		{
			destinationHost: "myhost.ns.cluster.local",
			svc:             "*.ns2.*",
			ans:             false,
		},
		{
			destinationHost: "myhost.ns.cluster.local",
			svc:             "myhost.ns2.cluster.local",
			ans:             false,
		},
		{
			destinationHost: "myhost.ns.cluster.local",
			svc:             "ns.*.svc.cluster",
			ans:             false,
		},
	} {
		t.Run(fmt.Sprintf("%s-%s", tst.destinationHost, tst.svc), func(t *testing.T) {
			ans := model.MatchesDestHost(tst.destinationHost, model.ConfigMeta{}, &mccpb.IstioService{
				Service: tst.svc,
			})
			if ans != tst.ans {
				t.Fatalf("want: %v, got: %v", tst.ans, ans)
			}
		})
	}
}

func TestIstioConfigStore_ServiceEntries(t *testing.T) {
	ns := "ns1"
	l := &fakeStore{
		cfg: map[resource.GroupVersionKind][]model.Config{
			gvk.ServiceEntry: {
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "request-count-1",
						Namespace: ns,
					},
					Spec: &networking.ServiceEntry{
						Hosts: []string{"*.googleapis.com"},
						Ports: []*networking.Port{
							{
								Name:     "https",
								Number:   443,
								Protocol: "HTTP",
							},
						},
					},
				},
			},
			gvk.QuotaSpec: {
				{
					ConfigMeta: model.ConfigMeta{
						Name:      "request-count-2",
						Namespace: ns,
					},
					Spec: &mccpb.QuotaSpec{
						Rules: []*mccpb.QuotaRule{
							{
								Quotas: []*mccpb.Quota{
									{
										Quota:  "requestcount",
										Charge: 100,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	ii := model.MakeIstioStore(l)
	cfgs := ii.ServiceEntries()

	if len(cfgs) != 1 {
		t.Fatalf("did not find 1 matched ServiceEntry, \n%v", cfgs)
	}
}

func TestIstioConfigStore_Gateway(t *testing.T) {
	workloadLabels := labels.Collection{}
	now := time.Now()
	gw1 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:              "name1",
			Namespace:         "zzz",
			CreationTimestamp: now,
		},
		Spec: &networking.Gateway{},
	}
	gw2 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:              "name1",
			Namespace:         "aaa",
			CreationTimestamp: now,
		},
		Spec: &networking.Gateway{},
	}
	gw3 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:              "name1",
			Namespace:         "ns2",
			CreationTimestamp: now.Add(time.Second * -1),
		},
		Spec: &networking.Gateway{},
	}

	l := &fakeStore{
		cfg: map[resource.GroupVersionKind][]model.Config{
			gvk.Gateway: {gw1, gw2, gw3},
		},
	}
	ii := model.MakeIstioStore(l)

	// Gateways should be returned in a stable order
	expectedConfig := []model.Config{
		gw3, // first config by timestamp
		gw2, // timestamp match with gw1, but name comes first
		gw1, // timestamp match with gw2, but name comes last
	}
	cfgs := ii.Gateways(workloadLabels)

	if !reflect.DeepEqual(expectedConfig, cfgs) {
		t.Errorf("Got different Config, Excepted:\n%v\n, Got: \n%v\n", expectedConfig, cfgs)
	}
}

func TestDeepCopy(t *testing.T) {
	cfg := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:              "name1",
			Namespace:         "zzz",
			CreationTimestamp: time.Now(),
		},
		Spec: &networking.Gateway{},
	}

	copied := cfg.DeepCopy()

	if !(cfg.Spec.String() == copied.Spec.String() &&
		cfg.Namespace == copied.Namespace &&
		cfg.Name == copied.Name &&
		cfg.CreationTimestamp == copied.CreationTimestamp) {
		t.Fatalf("cloned config is not identical")
	}

	// change the copied gateway to see if the original config is not effected
	copiedGateway := copied.Spec.(*networking.Gateway)
	copiedGateway.Selector = map[string]string{"app": "test"}

	gateway := cfg.Spec.(*networking.Gateway)
	if gateway.Selector != nil {
		t.Errorf("Original gateway is mutated")
	}
}
