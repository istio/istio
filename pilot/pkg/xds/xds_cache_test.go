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

package xds

import (
	"reflect"
	"testing"

	"github.com/golang/protobuf/ptypes/any"

	"istio.io/istio/pilot/pkg/model"
)

var (
	any1 = &any.Any{TypeUrl: "foo"}
	any2 = &any.Any{TypeUrl: "bar"}
)

func TestInMemoryCache(t *testing.T) {
	ep1 := EndpointBuilder{
		clusterName: "outbound|1||foo.com",
		service:     &model.Service{Hostname: "foo.com"},
	}
	ep2 := EndpointBuilder{
		clusterName: "outbound|2||foo.com",
		service:     &model.Service{Hostname: "foo.com"},
	}
	t.Run("simple", func(t *testing.T) {
		c := NewInMemoryCache()
		c.Insert(ep1, any1)
		if !reflect.DeepEqual(c.Keys(), []string{ep1.Key()}) {
			t.Fatalf("unexpected keys: %v, want %v", c.Keys(), ep1.Key())
		}
		if got, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}
		c.Insert(ep1, any2)
		if got, _ := c.Get(ep1); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.ClearHostnames(map[model.ConfigKey]struct{}{{Name: "foo.com"}: {}})
		if _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})

	t.Run("multiple hostnames", func(t *testing.T) {
		c := NewInMemoryCache()
		c.Insert(ep1, any1)
		c.Insert(ep2, any2)

		if got, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}
		if got, _ := c.Get(ep2); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.ClearHostnames(map[model.ConfigKey]struct{}{{Name: "foo.com"}: {}})
		if _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if _, f := c.Get(ep2); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})

	t.Run("multiple destinationRules", func(t *testing.T) {
		c := NewInMemoryCache()
		ep1 := ep1
		ep1.destinationRule = &model.Config{ConfigMeta: model.ConfigMeta{Name: "a", Namespace: "b"}}
		ep2 := ep2
		ep2.destinationRule = &model.Config{ConfigMeta: model.ConfigMeta{Name: "b", Namespace: "b"}}
		c.Insert(ep1, any1)
		c.Insert(ep2, any2)

		if got, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}
		if got, _ := c.Get(ep2); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.ClearDestinationRules(map[model.ConfigKey]struct{}{{Name: "a", Namespace: "b"}: {}})
		if _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if got, _ := c.Get(ep2); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.ClearDestinationRules(map[model.ConfigKey]struct{}{{Name: "b", Namespace: "b"}: {}})
		if _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if _, f := c.Get(ep2); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})

	t.Run("clear all", func(t *testing.T) {
		c := NewInMemoryCache()
		c.Insert(ep1, any1)
		c.Insert(ep2, any2)

		c.ClearAll()
		if len(c.Keys()) != 0 {
			t.Fatalf("expected no keys, got: %v", c.Keys())
		}
		if _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if _, f := c.Get(ep2); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})
}
