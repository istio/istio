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
		c.ClearHostnames(map[model.ConfigKey]struct{}{model.ConfigKey{Name: "foo.com"}: {}})
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
		c.ClearHostnames(map[model.ConfigKey]struct{}{model.ConfigKey{Name: "foo.com"}: {}})
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
		c.ClearDestinationRules(map[model.ConfigKey]struct{}{model.ConfigKey{Name: "a", Namespace: "b"}: {}})
		if _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if got, _ := c.Get(ep2); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.ClearDestinationRules(map[model.ConfigKey]struct{}{model.ConfigKey{Name: "b", Namespace: "b"}: {}})
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
