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
	"strconv"
	"testing"

	"github.com/davecgh/go-spew/spew"

	"istio.io/istio/pilot/pkg/model"
	mock_config "istio.io/istio/pilot/test/mock"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/util/sets"
)

// getByMessageName finds a schema by message name if it is available
// In test setup, we do not have more than one descriptor with the same message type, so this
// function is ok for testing purpose.
func getByMessageName(schemas collection.Schemas, name string) (resource.Schema, bool) {
	for _, s := range schemas.All() {
		if s.Proto() == name {
			return s, true
		}
	}
	return nil, false
}

func schemaFor(kind, proto string) resource.Schema {
	return resource.Builder{
		Kind:   kind,
		Plural: kind + "s",
		Proto:  proto,
	}.BuildNoValidate()
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

	aType, aExists := schemas.FindByGroupVersionKind(a.GroupVersionKind())
	if !aExists || !reflect.DeepEqual(aType, a) {
		t.Errorf("descriptor.GetByType(a) => got %+v, want %+v", aType, a)
	}
	if _, exists := schemas.FindByGroupVersionKind(config.GroupVersionKind{Kind: "missing"}); exists {
		t.Error("descriptor.GetByType(missing) => got true, want false")
	}

	aSchema, aSchemaExists := getByMessageName(schemas, a.Proto())
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
			a:    nil,
			b:    nil,
			want: true,
		},
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
		{
			a: labels.Instance{"a": "b"},
			b: labels.Instance{"a": "b", "c": "d"},
		},
		{
			b: labels.Instance{"a": "b", "c": "d"},
			a: labels.Instance{"a": "b"},
		},
		{
			b:    labels.Instance{"a": "b", "c": "d"},
			a:    labels.Instance{"a": "b", "c": "d"},
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
	want := "test.istio.io/v1/MockConfig/ns/mock-config2"
	if key := cfg.Meta.Key(); key != want {
		t.Fatalf("config.Key() => got %q, want %q", key, want)
	}
}

func TestResolveShortnameToFQDN(t *testing.T) {
	tests := []struct {
		name string
		meta config.Meta
		out  host.Name
	}{
		{
			"*", config.Meta{}, "*",
		},
		{
			"*", config.Meta{Namespace: "default", Domain: "cluster.local"}, "*",
		},
		{
			"foo", config.Meta{Namespace: "default", Domain: "cluster.local"}, "foo.default.svc.cluster.local",
		},
		{
			"foo.bar", config.Meta{Namespace: "default", Domain: "cluster.local"}, "foo.bar",
		},
		{
			"foo", config.Meta{Domain: "cluster.local"}, "foo.svc.cluster.local",
		},
		{
			"foo", config.Meta{Namespace: "default"}, "foo.default",
		},
		{
			"42.185.131.210", config.Meta{Namespace: "default"}, "42.185.131.210",
		},
		{
			"42.185.131.210", config.Meta{Namespace: "cluster.local"}, "42.185.131.210",
		},
		{
			"2a00:4000::614", config.Meta{Namespace: "default"}, "2a00:4000::614",
		},
		{
			"2a00:4000::614", config.Meta{Namespace: "cluster.local"}, "2a00:4000::614",
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
		specific := sets.New[host.Name]()
		wildcard := sets.New[host.Name]()
		for _, h := range tt.in {
			if h.IsWildCarded() {
				wildcard.Insert(h)
			} else {
				specific.Insert(h)
			}
		}

		t.Run(fmt.Sprintf("[%d] %s", idx, tt.needle), func(t *testing.T) {
			actual, value, found := model.MostSpecificHostMatch(tt.needle, specific, wildcard)
			if tt.want != "" && !found {
				t.Fatalf("model.MostSpecificHostMatch(%q, %v) = %v, %v, %t; want: %v", tt.needle, tt.in, actual, value, found, tt.want)
			} else if actual != tt.want {
				t.Fatalf("model.MostSpecificHostMatch(%q, %v) = %v, %v, %t; want: %v", tt.needle, tt.in, actual, value, found, tt.want)
			}
			if found {
				if actual.IsWildCarded() && value != wildcard[actual] {
					t.Fatalf("model.MostSpecificHostMatch(%q, %v) = %v, %v, %t; want: %v", tt.needle, tt.in, actual, value, found, tt.want)
				}
				if !actual.IsWildCarded() && value != specific[actual] {
					t.Fatalf("model.MostSpecificHostMatch(%q, %v) = %v, %v, %t; want: %v", tt.needle, tt.in, actual, value, found, tt.want)
				}
			}
		})
	}
}

func BenchmarkMostSpecificHostMatch(b *testing.B) {
	benchmarks := []struct {
		name             string
		needle           host.Name
		baseHost         string
		hosts            []host.Name
		specificHostsMap sets.Set[host.Name]
		wildcardHostsMap sets.Set[host.Name]
		time             int
		matches          bool
	}{
		{"10ExactNoMatch", host.Name("foo.bar.com.10"), "bar.com", []host.Name{}, nil, nil, 10, false},
		{"50ExactNoMatch", host.Name("foo.bar.com.50"), "bar.com", []host.Name{}, nil, nil, 50, false},
		{"100ExactNoMatch", host.Name("foo.bar.com.100"), "bar.com", []host.Name{}, nil, nil, 100, false},
		{"1000ExactNoMatch", host.Name("foo.bar.com.1000"), "bar.com", []host.Name{}, nil, nil, 1000, false},
		{"5000ExactNoMatch", host.Name("foo.bar.com.5000"), "bar.com", []host.Name{}, nil, nil, 5000, false},

		{"10ExactMatch", host.Name("foo.bar.com.10"), "foo.bar.com", []host.Name{}, nil, nil, 10, true},
		{"50ExactMatch", host.Name("foo.bar.com.50"), "foo.bar.com", []host.Name{}, nil, nil, 50, true},
		{"100ExactMatch", host.Name("foo.bar.com.100"), "foo.bar.com", []host.Name{}, nil, nil, 100, true},
		{"1000ExactMatch", host.Name("foo.bar.com.1000"), "foo.bar.com", []host.Name{}, nil, nil, 1000, true},
		{"5000ExactMatch", host.Name("foo.bar.com.5000"), "foo.bar.com", []host.Name{}, nil, nil, 5000, true},

		{"10DestRuleWildcardNoMatch", host.Name("foo.bar.com.10"), "*.foo.bar.com", []host.Name{}, nil, nil, 10, false},
		{"50DestRuleWildcardNoMatch", host.Name("foo.bar.com.50"), "*.foo.bar.com", []host.Name{}, nil, nil, 50, false},
		{"100DestRuleWildcardNoMatch", host.Name("foo.bar.com.100"), "*.foo.bar.com", []host.Name{}, nil, nil, 100, false},
		{"1000DestRuleWildcardNoMatch", host.Name("foo.bar.com.1000"), "*.foo.bar.com", []host.Name{}, nil, nil, 1000, false},
		{"5000DestRuleWildcardNoMatch", host.Name("foo.bar.com.5000"), "*.foo.bar.com", []host.Name{}, nil, nil, 5000, false},

		{"10DestRuleWildcardMatch", host.Name("foo.bar.baz.com.10"), "*.bar.baz.com", []host.Name{}, nil, nil, 10, true},
		{"50DestRuleWildcardMatch", host.Name("foo.bar.baz.com.50"), "*.bar.baz.com", []host.Name{}, nil, nil, 50, true},
		{"100DestRuleWildcardMatch", host.Name("foo.bar.baz.com.100"), "*.bar.baz.com", []host.Name{}, nil, nil, 100, true},
		{"1000DestRuleWildcardMatch", host.Name("foo.bar.baz.com.1000"), "*.bar.baz.com", []host.Name{}, nil, nil, 1000, true},
		{"5000DestRuleWildcardMatch", host.Name("foo.bar.baz.com.5000"), "*.bar.baz.com", []host.Name{}, nil, nil, 5000, true},

		{"10NeedleWildcardNoMatch", host.Name("*.bar.foo.bar.com"), "*.foo.bar.com", []host.Name{}, nil, nil, 10, false},
		{"50NeedleWildcardNoMatch", host.Name("*.bar.foo.bar.com"), "*.foo.bar.com", []host.Name{}, nil, nil, 50, false},
		{"100NeedleWildcardNoMatch", host.Name("*.bar.foo.bar.com"), "*.foo.bar.com", []host.Name{}, nil, nil, 100, false},
		{"1000NeedleWildcardNoMatch", host.Name("*.bar.foo.bar.com"), "*.foo.bar.com", []host.Name{}, nil, nil, 1000, false},
		{"5000NeedleWildcardNoMatch", host.Name("*.bar.foo.bar.com"), "*.foo.bar.com", []host.Name{}, nil, nil, 5000, false},

		{"10NeedleWildcardMatch", host.Name("*.bar.foo.bar.com.10"), "*.foo.bar.com", []host.Name{}, nil, nil, 10, true},
		{"50NeedleWildcardMatch", host.Name("*.bar.foo.bar.com.50"), "*.foo.bar.com", []host.Name{}, nil, nil, 50, true},
		{"100NeedleWildcardMatch", host.Name("*.bar.foo.bar.com.100"), "*.foo.bar.com", []host.Name{}, nil, nil, 100, true},
		{"1000NeedleWildcardMatch", host.Name("*.bar.foo.bar.com.1000"), "*.foo.bar.com", []host.Name{}, nil, nil, 1000, true},
		{"5000NeedleWildcardMatch", host.Name("*.bar.foo.bar.com.5000"), "*.foo.bar.com", []host.Name{}, nil, nil, 5000, true},
	}

	for _, bm := range benchmarks {
		bm.specificHostsMap = sets.NewWithLength[host.Name](bm.time)
		bm.wildcardHostsMap = sets.NewWithLength[host.Name](bm.time)

		for i := 1; i <= bm.time; i++ {
			h := host.Name(bm.baseHost + "." + strconv.Itoa(i))
			if h.IsWildCarded() {
				bm.wildcardHostsMap.Insert(h)
			} else {
				bm.specificHostsMap.Insert(h)
			}
		}

		b.Run(bm.name, func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, _, ok := model.MostSpecificHostMatch(bm.needle, bm.specificHostsMap, bm.wildcardHostsMap)
				if bm.matches != ok {
					b.Fatalf("expected to find match")
				}
			}
		})
	}
}

func BenchmarkMostSpecificHostMatchMixed(b *testing.B) {
	benchmarks := []struct {
		name             string
		needle           host.Name
		baseHost         string
		hosts            []host.Name
		specificHostsMap sets.Set[host.Name]
		wildcardHostsMap sets.Set[host.Name]
		time             int
		matches          bool
	}{
		{"10DestRuleWildcardNoMatch", host.Name("foo.bar.com.10"), "foo.bar.com", []host.Name{}, nil, nil, 10, false},
		{"50DestRuleWildcardNoMatch", host.Name("foo.bar.com.50"), "foo.bar.com", []host.Name{}, nil, nil, 50, false},
		{"100DestRuleWildcardNoMatch", host.Name("foo.bar.com.100"), "foo.bar.com", []host.Name{}, nil, nil, 100, false},
		{"1000DestRuleWildcardNoMatch", host.Name("foo.bar.com.1000"), "foo.bar.com", []host.Name{}, nil, nil, 1000, false},
		{"5000DestRuleWildcardNoMatch", host.Name("foo.bar.com.5000"), "foo.bar.com", []host.Name{}, nil, nil, 5000, false},

		{"10DestRuleWildcardMatch", host.Name("foo.bar.baz.com.10"), "bar.baz.com", []host.Name{}, nil, nil, 10, true},
		{"50DestRuleWildcardMatch", host.Name("foo.bar.baz.com.50"), "bar.baz.com", []host.Name{}, nil, nil, 50, true},
		{"100DestRuleWildcardMatch", host.Name("foo.bar.baz.com.100"), "bar.baz.com", []host.Name{}, nil, nil, 100, true},
		{"1000DestRuleWildcardMatch", host.Name("foo.bar.baz.com.1000"), "bar.baz.com", []host.Name{}, nil, nil, 1000, true},
		{"5000DestRuleWildcardMatch", host.Name("foo.bar.baz.com.5000"), "bar.baz.com", []host.Name{}, nil, nil, 5000, true},

		{"10NeedleWildcardNoMatch", host.Name("*.bar.foo.bar.com"), "foo.bar.com", []host.Name{}, nil, nil, 10, false},
		{"50NeedleWildcardNoMatch", host.Name("*.bar.foo.bar.com"), "foo.bar.com", []host.Name{}, nil, nil, 50, false},
		{"100NeedleWildcardNoMatch", host.Name("*.bar.foo.bar.com"), "foo.bar.com", []host.Name{}, nil, nil, 100, false},
		{"1000NeedleWildcardNoMatch", host.Name("*.bar.foo.bar.com"), "foo.bar.com", []host.Name{}, nil, nil, 1000, false},
		{"5000NeedleWildcardNoMatch", host.Name("*.bar.foo.bar.com"), "foo.bar.com", []host.Name{}, nil, nil, 5000, false},

		{"10NeedleWildcardMatch", host.Name("*.bar.foo.bar.com.10"), "foo.bar.com", []host.Name{}, nil, nil, 10, true},
		{"50NeedleWildcardMatch", host.Name("*.bar.foo.bar.com.50"), "foo.bar.com", []host.Name{}, nil, nil, 50, true},
		{"100NeedleWildcardMatch", host.Name("*.bar.foo.bar.com.100"), "foo.bar.com", []host.Name{}, nil, nil, 100, true},
		{"1000NeedleWildcardMatch", host.Name("*.bar.foo.bar.com.1000"), "foo.bar.com", []host.Name{}, nil, nil, 1000, true},
		{"5000NeedleWildcardMatch", host.Name("*.bar.foo.bar.com.5000"), "foo.bar.com", []host.Name{}, nil, nil, 5000, true},
	}

	for _, bm := range benchmarks {
		bm.specificHostsMap = make(map[host.Name]struct{}, bm.time)
		bm.wildcardHostsMap = make(map[host.Name]struct{}, bm.time)

		for i := 1; i <= bm.time; i++ {
			// these specific non-wildcard hosts are crafted this way to never match the needle,
			// this should replicate real-world scenarios of mixed specific and wildcard hosts
			specific := host.Name(strconv.Itoa(i) + "." + bm.baseHost)
			// generate correct wildcard hosts, one of these will match
			wildcard := host.Name("*." + bm.baseHost + "." + strconv.Itoa(i))

			bm.specificHostsMap[specific] = struct{}{}
			bm.wildcardHostsMap[wildcard] = struct{}{}
		}

		b.Run(bm.name, func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, _, ok := model.MostSpecificHostMatch(bm.needle, bm.specificHostsMap, bm.wildcardHostsMap)
				if bm.matches != ok {
					b.Fatalf("expected to find match")
				}
			}
		})
	}
}

func BenchmarkMostSpecificHostMatchMultiMatch(b *testing.B) {
	benchmarks := []struct {
		name             string
		needle           host.Name
		hosts            []host.Name
		specificHostsMap map[host.Name]struct{}
		wildcardHostsMap map[host.Name]struct{}
	}{
		{"DestRuleWildcard", host.Name("a.foo.bar.baz.com"), []host.Name{"*.foo.bar.baz.com", "*.bar.baz.com", "*.baz.com", "*.com"}, nil, nil},

		{"NeedleWildcard", host.Name("*.a.foo.bar.baz.com"), []host.Name{"*.foo.bar.baz.com", "*.bar.baz.com", "*.baz.com", "*.com"}, nil, nil},
	}

	for _, bm := range benchmarks {
		bm.specificHostsMap = sets.New[host.Name]()
		bm.wildcardHostsMap = sets.NewWithLength[host.Name](len(bm.hosts))

		for _, h := range bm.hosts {
			if h.IsWildCarded() {
				bm.wildcardHostsMap[h] = struct{}{}
			} else {
				bm.specificHostsMap[h] = struct{}{}
			}
		}

		b.Run(bm.name, func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, _, ok := model.MostSpecificHostMatch(bm.needle, bm.specificHostsMap, bm.wildcardHostsMap)
				if !ok {
					b.Fatalf("expected to find match")
				}
			}
		})
	}
}

func BenchmarkHashCode(b *testing.B) {
	benchmarks := []struct {
		name   string
		config model.ConfigKey
	}{
		{
			name: "small string",
			config: model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "abc",
				Namespace: "ns-foo",
			},
		},
		{
			name: "middle string",
			config: model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "foo.svc.cluster.local.middle.len",
				Namespace: "ns-foo-a-middle-string-with-len",
			},
		},
		{
			name: "long string",
			config: model.ConfigKey{
				Kind:      kind.VirtualService,
				Name:      "foo.svc.cluster.local.middle.len.foo.svc.cluster.local.middle.len",
				Namespace: "ns-foo-a-middle-string-with-len.ns-foo-a-middle-string-with-len",
			},
		},
	}
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				bm.config.HashCode()
			}
		})
	}
}

func TestHashCodeCollision(t *testing.T) {
	config1 := model.ConfigKey{
		Kind:      kind.VirtualService,
		Name:      "abc",
		Namespace: "ns-foo",
	}

	config2 := model.ConfigKey{
		Kind:      kind.VirtualService,
		Name:      "ab",
		Namespace: "cns-foo",
	}

	if config1.HashCode() == config2.HashCode() {
		t.Fatalf("Hash code of config1 %s should not be equal to config2 %s", config1.String(), config2.String())
	}
}
