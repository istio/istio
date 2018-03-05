// Copyright 2018 Istio Authors.
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

package crd

import (
	"fmt"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
)

func oneCallConversion(key string, out *model.Config) ObjectConverter {
	called := false
	return func(schema model.ProtoSchema, object IstioObject, domain string) (*model.Config, error) {
		if model.Key(schema.Type, object.GetObjectMeta().Name, domain) != key {
			return nil, fmt.Errorf("wrong key")
		}
		if !called {
			called = true
			return out, nil
		}
		panic("can only be called once")
	}
}

func TestConversionCache(t *testing.T) {
	schema := model.Gateway
	obj := &Gateway{
		ObjectMeta: v1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
		},
		Spec: map[string]interface{}{
			"servers": []interface{}{v1alpha3.Server{
				Port:  &v1alpha3.Port{Number: 7},
				Hosts: []string{"host"},
			}},
		},
	}
	// call the real conversion method to get our expected output
	want, _ := ConvertObject(schema, obj, "bar")

	key := model.Key(schema.Type, obj.GetObjectMeta().Name, obj.GetObjectMeta().Namespace)
	underTest := NewCachingConverter(oneCallConversion(key, want))

	// attempt to convert obj the first time
	out, err := underTest.ConvertObject(schema, obj, "bar")
	if err != nil || !reflect.DeepEqual(out, want) {
		t.Fatalf("underTest.ConvertObject(%v, %v, %q) = %v, %v, wanted %v", schema, obj, "bar", out, err, want)
	}
	// verify we get the same object a second time, without calling the underlying converter (which will panic if called a second time)
	out2, err := underTest.ConvertObject(schema, obj, "bar")
	if err != nil || !reflect.DeepEqual(out2, out) {
		t.Fatalf("underTest.ConvertObject(%v, %v, %q) = %v, %v, wanted %v", schema, obj, "bar", out2, err, out)
	}

	// finally, verify that the cache does call the underlying converter after an eviction
	underTest.Evict(key)
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("Should have panic'd after evicting %q from cache and attempting to convert a third time", key)
		}
	}()
	_, _ = underTest.ConvertObject(schema, obj, "bar")
	t.Fatalf("unreachable")
}
