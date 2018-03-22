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

package mock

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/golang/protobuf/proto"

	authn "istio.io/api/authentication/v1alpha2"
	mpb "istio.io/api/mixer/v1"
	mccpb "istio.io/api/mixer/v1/config/client"
	networking "istio.io/api/networking/v1alpha3"
	rbac "istio.io/api/rbac/v1alpha1"
	routing "istio.io/api/routing/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/model/test"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/log"
)

var (
	// Types defines the mock config descriptor
	Types = model.ConfigDescriptor{model.MockConfig}

	// ExampleRouteRule is an example route rule
	ExampleRouteRule = &routing.RouteRule{
		Destination: &routing.IstioService{
			Name: "world",
		},
		Route: []*routing.DestinationWeight{
			{Weight: 80, Labels: map[string]string{"version": "v1"}},
			{Weight: 20, Labels: map[string]string{"version": "v2"}},
		},
	}

	// ExampleVirtualService is an example V2 route rule
	ExampleVirtualService = &networking.VirtualService{
		Hosts: []string{"prod", "test"},
		Http: []*networking.HTTPRoute{
			{
				Route: []*networking.DestinationWeight{
					{
						Destination: &networking.Destination{
							Name: "job",
						},
						Weight: 80,
					},
				},
			},
		},
	}

	ExampleExternalService = &networking.ExternalService{
		Hosts:     []string{"*.google.com"},
		Discovery: networking.ExternalService_NONE,
		Ports: []*networking.Port{
			{Number: 80, Name: "http-name", Protocol: "http"},
			{Number: 8080, Name: "http2-name", Protocol: "http2"},
		},
	}

	ExampleGateway = &networking.Gateway{
		Servers: []*networking.Server{
			{
				Hosts: []string{"google.com"},
				Port:  &networking.Port{Name: "http", Protocol: "http", Number: 10080},
			},
		},
	}

	// ExampleDestinationRule is an example destination rule
	ExampleDestinationRule = &networking.DestinationRule{
		Name: "ratings",
		TrafficPolicy: &networking.TrafficPolicy{
			LoadBalancer: &networking.LoadBalancerSettings{
				new(networking.LoadBalancerSettings_Simple),
			},
		},
	}

	// ExampleIngressRule is an example ingress rule
	ExampleIngressRule = &routing.IngressRule{
		Destination: &routing.IstioService{
			Name: "world",
		},
		Port: 80,
		DestinationServicePort: &routing.IngressRule_DestinationPort{DestinationPort: 80},
	}

	// ExampleEgressRule is an example egress rule
	ExampleEgressRule = &routing.EgressRule{
		Destination: &routing.IstioService{
			Service: "*cnn.com",
		},
		Ports:          []*routing.EgressRule_Port{{Port: 80, Protocol: "http"}},
		UseEgressProxy: false,
	}

	// ExampleDestinationPolicy is an example destination policy
	ExampleDestinationPolicy = &routing.DestinationPolicy{
		Destination: &routing.IstioService{
			Name: "world",
		},
		LoadBalancing: &routing.LoadBalancing{
			LbPolicy: &routing.LoadBalancing_Name{Name: routing.LoadBalancing_RANDOM},
		},
	}

	// ExampleHTTPAPISpec is an example HTTPAPISpec
	ExampleHTTPAPISpec = &mccpb.HTTPAPISpec{
		Attributes: &mpb.Attributes{
			Attributes: map[string]*mpb.Attributes_AttributeValue{
				"api.service": {Value: &mpb.Attributes_AttributeValue_StringValue{"petstore"}},
			},
		},
		Patterns: []*mccpb.HTTPAPISpecPattern{{
			Attributes: &mpb.Attributes{
				Attributes: map[string]*mpb.Attributes_AttributeValue{
					"api.operation": {Value: &mpb.Attributes_AttributeValue_StringValue{"getPet"}},
				},
			},
			HttpMethod: "GET",
			Pattern: &mccpb.HTTPAPISpecPattern_UriTemplate{
				UriTemplate: "/pets/{id}",
			},
		}},
		ApiKeys: []*mccpb.APIKey{{
			Key: &mccpb.APIKey_Header{
				Header: "X-API-KEY",
			},
		}},
	}

	// ExampleHTTPAPISpecBinding is an example HTTPAPISpecBinding
	ExampleHTTPAPISpecBinding = &mccpb.HTTPAPISpecBinding{
		Services: []*mccpb.IstioService{
			{
				Name:      "foo",
				Namespace: "bar",
			},
		},
		ApiSpecs: []*mccpb.HTTPAPISpecReference{
			{
				Name:      "petstore",
				Namespace: "default",
			},
		},
	}

	// ExampleQuotaSpec is an example QuotaSpec
	ExampleQuotaSpec = &mccpb.QuotaSpec{
		Rules: []*mccpb.QuotaRule{{
			Match: []*mccpb.AttributeMatch{{
				Clause: map[string]*mccpb.StringMatch{
					"api.operation": {
						MatchType: &mccpb.StringMatch_Exact{
							Exact: "getPet",
						},
					},
				},
			}},
			Quotas: []*mccpb.Quota{{
				Quota:  "fooQuota",
				Charge: 2,
			}},
		}},
	}

	// ExampleQuotaSpecBinding is an example QuotaSpecBinding
	ExampleQuotaSpecBinding = &mccpb.QuotaSpecBinding{
		Services: []*mccpb.IstioService{
			{
				Name:      "foo",
				Namespace: "bar",
			},
		},
		QuotaSpecs: []*mccpb.QuotaSpecBinding_QuotaSpecReference{
			{
				Name:      "fooQuota",
				Namespace: "default",
			},
		},
	}

	// ExampleEndUserAuthenticationPolicySpec is an example EndUserAuthenticationPolicySpec
	ExampleEndUserAuthenticationPolicySpec = &mccpb.EndUserAuthenticationPolicySpec{
		Jwts: []*mccpb.JWT{
			{
				Issuer: "https://issuer.example.com",
				Audiences: []string{
					"audience_foo.example.com",
					"audience_bar.example.com",
				},
				JwksUri:                "https://www.example.com/oauth/v1/certs",
				ForwardJwt:             true,
				PublicKeyCacheDuration: types.DurationProto(5 * time.Minute),
				Locations: []*mccpb.JWT_Location{{
					Scheme: &mccpb.JWT_Location_Header{
						Header: "x-goog-iap-jwt-assertion",
					},
				}},
			},
		},
	}

	// ExampleEndUserAuthenticationPolicySpecBinding is an example EndUserAuthenticationPolicySpecBinding
	ExampleEndUserAuthenticationPolicySpecBinding = &mccpb.EndUserAuthenticationPolicySpecBinding{
		Services: []*mccpb.IstioService{
			{
				Name:      "foo",
				Namespace: "bar",
			},
		},
		Policies: []*mccpb.EndUserAuthenticationPolicySpecReference{
			{
				Name:      "fooPolicy",
				Namespace: "default",
			},
		},
	}

	// ExampleAuthenticationPolicy is an example authentication Policy
	ExampleAuthenticationPolicy = &authn.Policy{
		Targets: []*authn.TargetSelector{{
			Name: "hello",
		}},
		Peers: []*authn.PeerAuthenticationMethod{{
			Params: &authn.PeerAuthenticationMethod_Mtls{},
		}},
	}

	// ExampleServiceRole is an example rbac service role
	ExampleServiceRole = &rbac.ServiceRole{Rules: []*rbac.AccessRule{
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
	}}

	// ExampleServiceRoleBinding is an example rbac service role binding
	ExampleServiceRoleBinding = &rbac.ServiceRoleBinding{
		Subjects: []*rbac.Subject{
			{User: "User0", Group: "Group0", Properties: map[string]string{"prop0": "value0"}},
			{User: "User1", Group: "Group1", Properties: map[string]string{"prop1": "value1"}},
		},
		RoleRef: &rbac.RoleRef{Kind: "ServiceRole", Name: "ServiceRole001"},
	}
)

// Make creates a mock config indexed by a number
func Make(namespace string, i int) model.Config {
	name := fmt.Sprintf("%s%d", "mock-config", i)
	return model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:      model.MockConfig.Type,
			Group:     "test.istio.io",
			Version:   "v1",
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"key": name,
			},
			Annotations: map[string]string{
				"annotationkey": name,
			},
		},
		Spec: &test.MockConfig{
			Key: name,
			Pairs: []*test.ConfigPair{
				{Key: "key", Value: strconv.Itoa(i)},
			},
		},
	}
}

// Compare checks two configs ignoring revisions
func Compare(a, b model.Config) bool {
	a.ResourceVersion = ""
	b.ResourceVersion = ""
	return reflect.DeepEqual(a, b)
}

// CheckMapInvariant validates operational invariants of an empty config registry
func CheckMapInvariant(r model.ConfigStore, t *testing.T, namespace string, n int) {
	// check that the config descriptor is the mock config descriptor
	_, contains := r.ConfigDescriptor().GetByType(model.MockConfig.Type)
	if !contains {
		t.Error("expected config mock types")
	}

	// create configuration objects
	elts := make(map[int]model.Config)
	for i := 0; i < n; i++ {
		elts[i] = Make(namespace, i)
	}

	// post all elements
	for _, elt := range elts {
		if _, err := r.Create(elt); err != nil {
			t.Error(err)
		}
	}

	revs := make(map[int]string)

	// check that elements are stored
	for i, elt := range elts {
		v1, ok := r.Get(model.MockConfig.Type, elt.Name, elt.Namespace)
		if !ok || !Compare(elt, *v1) {
			t.Errorf("wanted %v, got %v", elt, v1)
		} else {
			revs[i] = v1.ResourceVersion
		}
	}

	if _, err := r.Create(elts[0]); err == nil {
		t.Error("expected error posting twice")
	}

	invalid := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:            model.MockConfig.Type,
			Name:            "invalid",
			ResourceVersion: revs[0],
		},
		Spec: &test.MockConfig{},
	}

	missing := model.Config{
		ConfigMeta: model.ConfigMeta{
			Type:            model.MockConfig.Type,
			Name:            "missing",
			ResourceVersion: revs[0],
		},
		Spec: &test.MockConfig{Key: "missing"},
	}

	if _, err := r.Create(model.Config{}); err == nil {
		t.Error("expected error posting empty object")
	}

	if _, err := r.Create(invalid); err == nil {
		t.Error("expected error posting invalid object")
	}

	if _, err := r.Update(model.Config{}); err == nil {
		t.Error("expected error updating empty object")
	}

	if _, err := r.Update(invalid); err == nil {
		t.Error("expected error putting invalid object")
	}

	if _, err := r.Update(missing); err == nil {
		t.Error("expected error putting missing object with a missing key")
	}

	if _, err := r.Update(elts[0]); err == nil {
		t.Error("expected error putting object without revision")
	}

	badrevision := elts[0]
	badrevision.ResourceVersion = "bad"

	if _, err := r.Update(badrevision); err == nil {
		t.Error("expected error putting object with a bad revision")
	}

	// check for missing type
	if l, _ := r.List("missing", namespace); len(l) > 0 {
		t.Errorf("unexpected objects for missing type")
	}

	// check for missing element
	if _, ok := r.Get(model.MockConfig.Type, "missing", ""); ok {
		t.Error("unexpected configuration object found")
	}

	// check for missing element
	if _, ok := r.Get("missing", "missing", ""); ok {
		t.Error("unexpected configuration object found")
	}

	// delete missing elements
	if err := r.Delete("missing", "missing", ""); err == nil {
		t.Error("expected error on deletion of missing type")
	}

	// delete missing elements
	if err := r.Delete(model.MockConfig.Type, "missing", ""); err == nil {
		t.Error("expected error on deletion of missing element")
	}
	if err := r.Delete(model.MockConfig.Type, "missing", "unknown"); err == nil {
		t.Error("expected error on deletion of missing element in unknown namespace")
	}

	// list elements
	l, err := r.List(model.MockConfig.Type, namespace)
	if err != nil {
		t.Errorf("List error %#v, %v", l, err)
	}
	if len(l) != n {
		t.Errorf("wanted %d element(s), got %d in %v", n, len(l), l)
	}

	// update all elements
	for i := 0; i < n; i++ {
		elt := Make(namespace, i)
		elt.Spec.(*test.MockConfig).Pairs[0].Value += "(updated)"
		elt.ResourceVersion = revs[i]
		elts[i] = elt
		if _, err = r.Update(elt); err != nil {
			t.Error(err)
		}
	}

	// check that elements are stored
	for i, elt := range elts {
		v1, ok := r.Get(model.MockConfig.Type, elts[i].Name, elts[i].Namespace)
		if !ok || !Compare(elt, *v1) {
			t.Errorf("wanted %v, got %v", elt, v1)
		}
	}

	// delete all elements
	for i := range elts {
		if err = r.Delete(model.MockConfig.Type, elts[i].Name, elts[i].Namespace); err != nil {
			t.Error(err)
		}
	}

	l, err = r.List(model.MockConfig.Type, namespace)
	if err != nil {
		t.Error(err)
	}
	if len(l) != 0 {
		t.Errorf("wanted 0 element(s), got %d in %v", len(l), l)
	}
}

// CheckIstioConfigTypes validates that an empty store can ingest Istio config objects
func CheckIstioConfigTypes(store model.ConfigStore, namespace string, t *testing.T) {
	name := "example"

	cases := []struct {
		name   string
		schema model.ProtoSchema
		spec   proto.Message
	}{
		{"RouteRule", model.RouteRule, ExampleRouteRule},
		{"VirtualService", model.VirtualService, ExampleVirtualService},
		{"DestinationRule", model.DestinationRule, ExampleDestinationRule},
		{"ExternalService", model.ExternalService, ExampleExternalService},
		{"Gatway", model.Gateway, ExampleGateway},
		{"IngressRule", model.IngressRule, ExampleIngressRule},
		{"EgressRule", model.EgressRule, ExampleEgressRule},
		{"DestinationPolicy", model.DestinationPolicy, ExampleDestinationPolicy},
		{"HTTPAPISpec", model.HTTPAPISpec, ExampleHTTPAPISpec},
		{"HTTPAPISpecBinding", model.HTTPAPISpecBinding, ExampleHTTPAPISpecBinding},
		{"QuotaSpec", model.QuotaSpec, ExampleQuotaSpec},
		{"QuotaSpecBinding", model.QuotaSpecBinding, ExampleQuotaSpecBinding},
		{"EndUserAuthenticationPolicySpec", model.EndUserAuthenticationPolicySpec,
			ExampleEndUserAuthenticationPolicySpec},
		{"EndUserAuthenticationPolicySpecBinding", model.EndUserAuthenticationPolicySpecBinding,
			ExampleEndUserAuthenticationPolicySpecBinding},
		{"Policy", model.AuthenticationPolicy, ExampleAuthenticationPolicy},
		{"ServiceRole", model.ServiceRole, ExampleServiceRole},
		{"ServiceRoleBinding", model.ServiceRoleBinding, ExampleServiceRoleBinding},
	}

	for _, c := range cases {
		if _, err := store.Create(model.Config{
			ConfigMeta: model.ConfigMeta{
				Type:      c.schema.Type,
				Name:      name,
				Group:     c.schema.Group + model.IstioAPIGroupDomain,
				Version:   c.schema.Version,
				Namespace: namespace,
			},
			Spec: c.spec,
		}); err != nil {
			t.Errorf("Post(%v) => got %v", c.name, err)
		}
	}
}

// CheckCacheEvents validates operational invariants of a cache
func CheckCacheEvents(store model.ConfigStore, cache model.ConfigStoreCache, namespace string, n int, t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	ach, dch := make(chan bool, n), make(chan bool, n)
	defer close(ach)
	defer close(dch)
	sad, sdd := 0, 0
	cache.RegisterEventHandler(model.MockConfig.Type, func(c model.Config, ev model.Event) {
		switch ev {
		case model.EventAdd:
			if sdd != 0 {
				t.Errorf("Events are not serialized (add)")
			}
			sad++
			ach <- true
		case model.EventDelete:
			if sad != n {
				t.Errorf("Events are not serialized (delete)")
			}
			sdd++
			dch <- true
		}
		log.Infof("Added %d, deleted %d", sad, sdd)
	})
	go cache.Run(stop)

	// run map invariant sequence
	CheckMapInvariant(store, t, namespace, n)

	log.Infof("Waiting till all events are received")
	timeout := time.After(60 * time.Second)
	added, deleted := 0, 0
	for {
		select {
		case <-timeout:
			t.Fatalf("timeout waiting to receive expected events. actual added %d deleted %d. expected %d",
				added, deleted, n)
		case <-ach:
			added++
		case <-dch:
			deleted++
		default:
			if added == n && deleted == n {
				return
			}
		}
	}
}

// CheckCacheFreshness validates operational invariants of a cache
func CheckCacheFreshness(cache model.ConfigStoreCache, namespace string, t *testing.T) {
	stop := make(chan struct{})
	done := make(chan bool)
	o := Make(namespace, 0)

	// validate cache consistency
	cache.RegisterEventHandler(model.MockConfig.Type, func(config model.Config, ev model.Event) {
		elts, _ := cache.List(model.MockConfig.Type, namespace)
		elt, exists := cache.Get(o.Type, o.Name, o.Namespace)
		switch ev {
		case model.EventAdd:
			if len(elts) != 1 {
				t.Errorf("Got %#v, expected %d element(s) on Add event", elts, 1)
			}
			if !exists || elt == nil || !reflect.DeepEqual(elt.Spec, o.Spec) {
				t.Errorf("Got %#v, %t, expected %#v", elt, exists, o)
			}

			log.Infof("Calling Update(%s)", config.Key())
			revised := Make(namespace, 1)
			revised.ConfigMeta = elt.ConfigMeta
			if _, err := cache.Update(revised); err != nil {
				t.Error(err)
			}
		case model.EventUpdate:
			if len(elts) != 1 {
				t.Errorf("Got %#v, expected %d element(s) on Update event", elts, 1)
			}
			if !exists || elt == nil {
				t.Errorf("Got %#v, %t, expected nonempty", elt, exists)
			}

			log.Infof("Calling Delete(%s)", config.Key())
			if err := cache.Delete(model.MockConfig.Type, config.Name, config.Namespace); err != nil {
				t.Error(err)
			}
		case model.EventDelete:
			if len(elts) != 0 {
				t.Errorf("Got %#v, expected zero elements on Delete event", elts)
			}
			log.Infof("Stopping channel for (%#v)", config.Key)
			close(stop)
			done <- true
		}
	})

	go cache.Run(stop)

	// try warm-up with empty Get
	if _, exists := cache.Get("unknown", "example", namespace); exists {
		t.Error("unexpected result for unknown type")
	}

	// add and remove
	log.Infof("Calling Create(%#v)", o)
	if _, err := cache.Create(o); err != nil {
		t.Error(err)
	}

	timeout := time.After(10 * time.Second)
	select {
	case <-timeout:
		t.Fatalf("timeout waiting to be done")
	case <-done:
		return
	}
}

// CheckCacheSync validates operational invariants of a cache against the
// non-cached client.
func CheckCacheSync(store model.ConfigStore, cache model.ConfigStoreCache, namespace string, n int, t *testing.T) {
	keys := make(map[int]model.Config)
	// add elements directly through client
	for i := 0; i < n; i++ {
		keys[i] = Make(namespace, i)
		if _, err := store.Create(keys[i]); err != nil {
			t.Error(err)
		}
	}

	// check in the controller cache
	stop := make(chan struct{})
	defer close(stop)
	go cache.Run(stop)
	util.Eventually("HasSynced", cache.HasSynced, t)
	os, _ := cache.List(model.MockConfig.Type, namespace)
	if len(os) != n {
		t.Errorf("cache.List => Got %d, expected %d", len(os), n)
	}

	// remove elements directly through client
	for i := 0; i < n; i++ {
		if err := store.Delete(model.MockConfig.Type, keys[i].Name, keys[i].Namespace); err != nil {
			t.Error(err)
		}
	}

	// check again in the controller cache
	util.Eventually("no elements in cache", func() bool {
		os, _ = cache.List(model.MockConfig.Type, namespace)
		log.Infof("cache.List => Got %d, expected %d", len(os), 0)
		return len(os) == 0
	}, t)

	// now add through the controller
	for i := 0; i < n; i++ {
		if _, err := cache.Create(Make(namespace, i)); err != nil {
			t.Error(err)
		}
	}

	// check directly through the client
	util.Eventually("cache and backing store match", func() bool {
		cs, _ := cache.List(model.MockConfig.Type, namespace)
		os, _ := store.List(model.MockConfig.Type, namespace)
		log.Infof("cache.List => Got %d, expected %d", len(cs), n)
		log.Infof("store.List => Got %d, expected %d", len(os), n)
		return len(os) == n && len(cs) == n
	}, t)

	// remove elements directly through the client
	for i := 0; i < n; i++ {
		if err := store.Delete(model.MockConfig.Type, keys[i].Name, keys[i].Namespace); err != nil {
			t.Error(err)
		}
	}
}
