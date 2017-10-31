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
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/golang/protobuf/proto"

	brokerconfig "istio.io/api/broker/v1/config"
	"istio.io/broker/pkg/model/config"
	testproto "istio.io/broker/pkg/testing/mock/proto"
)

var (
	// FakeConfig is used purely for testing.
	FakeConfig = config.Schema{
		Type:        "fake-config",
		Plural:      "fake-configs",
		MessageName: "proto.FakeConfig",
		AdditionalValidate: func(msg proto.Message) error {
			if msg.(*testproto.FakeConfig).Key == "" {
				return errors.New("empty key")
			}
			return nil
		},
	}
)

// Make creates a fake config indexed by a number
func Make(namespace string, i int) config.Entry {
	name := fmt.Sprintf("%s%d", "fake-config", i)
	return config.Entry{
		Meta: config.Meta{
			Type:      FakeConfig.Type,
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"key": name,
			},
			Annotations: map[string]string{
				"annotationkey": name,
			},
		},
		Spec: &testproto.FakeConfig{
			Key: name,
			Pairs: []*testproto.Pair{
				{Key: "key", Value: strconv.Itoa(i)},
			},
		},
	}
}

// Compare checks two configs ignoring revisions
func Compare(a, b config.Entry) bool {
	a.ResourceVersion = ""
	b.ResourceVersion = ""
	return reflect.DeepEqual(a, b)
}

// CheckMapInvariant validates operational invariants of an empty config store
func CheckMapInvariant(r config.Store, t *testing.T, namespace string, n int) {
	// check that the config descriptor is the mock config descriptor
	_, contains := r.Descriptor().GetByType(FakeConfig.Type)
	if !contains {
		t.Error("expected config mock types")
	}

	// create configuration objects
	elts := make(map[int]config.Entry)
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
		v1, ok := r.Get(FakeConfig.Type, elt.Name, elt.Namespace)
		if !ok || !Compare(elt, *v1) {
			t.Errorf("wanted %v, got %v", elt, v1)
		} else {
			revs[i] = v1.ResourceVersion
		}
	}

	if _, err := r.Create(elts[0]); err == nil {
		t.Error("expected error posting twice")
	}

	invalid := config.Entry{
		Meta: config.Meta{
			Type:            FakeConfig.Type,
			Name:            "invalid",
			ResourceVersion: revs[0],
		},
		Spec: &testproto.FakeConfig{},
	}

	missing := config.Entry{
		Meta: config.Meta{
			Type:            FakeConfig.Type,
			Name:            "missing",
			ResourceVersion: revs[0],
		},
		Spec: &testproto.FakeConfig{Key: "missing"},
	}

	if _, err := r.Create(invalid); err == nil {
		t.Error("expected error posting invalid object")
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
	if _, ok := r.Get(FakeConfig.Type, "missing", ""); ok {
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
	if err := r.Delete(FakeConfig.Type, "missing", ""); err == nil {
		t.Error("expected error on deletion of missing element")
	}

	// list elements
	l, err := r.List(FakeConfig.Type, namespace)
	if err != nil {
		t.Errorf("List error %#v, %v", l, err)
	}
	if len(l) != n {
		t.Errorf("wanted %d element(s), got %d in %v", n, len(l), l)
	}

	// update all elements
	for i := 0; i < n; i++ {
		elt := Make(namespace, i)
		elt.Spec.(*testproto.FakeConfig).Pairs[0].Value += "(updated)"
		elt.ResourceVersion = revs[i]
		elts[i] = elt
		if _, err = r.Update(elt); err != nil {
			t.Error(err)
		}
	}

	// check that elements are stored
	for i, elt := range elts {
		v1, ok := r.Get(FakeConfig.Type, elts[i].Name, elts[i].Namespace)
		if !ok || !Compare(elt, *v1) {
			t.Errorf("wanted %v, got %v", elt, v1)
		}
	}

	// delete all elements
	for i := range elts {
		if err = r.Delete(FakeConfig.Type, elts[i].Name, elts[i].Namespace); err != nil {
			t.Error(err)
		}
	}

	l, err = r.List(FakeConfig.Type, namespace)
	if err != nil {
		t.Error(err)
	}
	if len(l) != 0 {
		t.Errorf("wanted 0 element(s), got %d in %v", len(l), l)
	}
}

// CheckBrokerConfigTypes validates that an empty store can ingest Istio config objects
func CheckBrokerConfigTypes(store config.Store, namespace string, t *testing.T) {
	name := "example"

	cases := []config.Entry{
		{
			Meta: config.Meta{
				Type:      config.ServiceClass.Type,
				Name:      name,
				Namespace: namespace,
			},
			Spec: &brokerconfig.ServiceClass{
				Deployment: &brokerconfig.Deployment{
					Instance: "productpage",
				},
				Entry: &brokerconfig.CatalogEntry{
					Name:        "istio-bookinfo-productpage",
					Id:          "4395a443-f49a-41b0-8d14-d17294cf612f",
					Description: "A book info service",
				},
			},
		},
		{
			Meta: config.Meta{
				Type:      config.ServicePlan.Type,
				Name:      name,
				Namespace: namespace,
			},
			Spec: &brokerconfig.ServicePlan{
				Plan: &brokerconfig.CatalogPlan{
					Name:        "istio-monthly",
					Id:          "58646b26-867a-4954-a1b9-233dac07815b",
					Description: "monthly subscription",
				},
				Services: []string{
					"productpage-service-class",
				},
			},
		},
	}

	for _, c := range cases {
		typ := c.Meta.Type
		if _, err := store.Create(c); err != nil {
			t.Errorf("Create(%s) => got %v", typ, err)
		}
		got, ok := store.Get(typ, name, namespace)
		if !ok {
			t.Errorf("Get(%s) => not found", typ)
		}
		if !reflect.DeepEqual(got.Spec, c.Spec) {
			t.Errorf("Get(%s): got %+vwant %+v", typ, spew.Sdump(got), spew.Sdump(c))
		}
	}
}
