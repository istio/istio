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
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes/any"
	"go.uber.org/atomic"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	any1 = &any.Any{TypeUrl: "foo"}
	any2 = &any.Any{TypeUrl: "bar"}
)

// TestXdsCacheToken is a regression test to ensure that we do not write
func TestXdsCacheToken(t *testing.T) {
	c := model.NewXdsCache()
	n := atomic.NewInt32(0)
	mkv := func(n int32) *any.Any {
		return &any.Any{TypeUrl: fmt.Sprint(n)}
	}
	k := EndpointBuilder{clusterName: "key", service: &model.Service{Hostname: "foo.com"}}
	work := func() {
		_, tok, f := c.Get(k)
		if f {
			return
		}
		v := mkv(n.Load())
		time.Sleep(time.Millisecond * time.Duration(rand.Intn(100)))
		c.Add(k, tok, v)
	}
	for vals := 0; vals < 5; vals++ {
		for i := 0; i < 5; i++ {
			go work()
		}
		retry.UntilOrFail(t, func() bool {
			val, _, f := c.Get(k)
			return f && val.TypeUrl == fmt.Sprint(n)
		})
		n.Inc()
		c.ClearAll()
		for i := 0; i < 5; i++ {
			val, _, f := c.Get(k)
			if f {
				t.Log("found unexpected write", val.TypeUrl)
			}
			if f && val.TypeUrl != fmt.Sprint(n) {
				t.Fatalf("got bad write: %v", val.TypeUrl)
			}
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(20)))
		}
	}
}

func TestXdsCache(t *testing.T) {
	ep1 := EndpointBuilder{
		clusterName: "outbound|1||foo.com",
		service:     &model.Service{Hostname: "foo.com"},
	}
	ep2 := EndpointBuilder{
		clusterName: "outbound|2||foo.com",
		service:     &model.Service{Hostname: "foo.com"},
	}
	addWithToken := func(c model.XdsCache, entry model.XdsCacheEntry, value *any.Any) {
		_, tok, _ := c.Get(entry)
		c.Add(entry, tok, value)
	}
	t.Run("simple", func(t *testing.T) {
		c := model.NewLenientXdsCache()

		addWithToken(c, ep1, any1)
		if !reflect.DeepEqual(c.Keys(), []string{ep1.Key()}) {
			t.Fatalf("unexpected keys: %v, want %v", c.Keys(), ep1.Key())
		}
		if got, _, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}

		addWithToken(c, ep1, any2)
		if got, _, _ := c.Get(ep1); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}

		c.Clear(map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "foo.com"}: {}})
		if _, _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})

	t.Run("multiple hostnames", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		addWithToken(c, ep1, any1)
		addWithToken(c, ep2, any2)

		if got, _, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}
		if got, _, _ := c.Get(ep2); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.Clear(map[model.ConfigKey]struct{}{{Kind: gvk.ServiceEntry, Name: "foo.com"}: {}})
		if _, _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if _, _, f := c.Get(ep2); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})

	t.Run("multiple destinationRules", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		ep1 := ep1
		ep1.destinationRule = &config.Config{Meta: config.Meta{Name: "a", Namespace: "b"}}
		ep2 := ep2
		ep2.destinationRule = &config.Config{Meta: config.Meta{Name: "b", Namespace: "b"}}
		addWithToken(c, ep1, any1)
		addWithToken(c, ep2, any2)
		if got, _, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}
		if got, _, _ := c.Get(ep2); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.Clear(map[model.ConfigKey]struct{}{{Kind: gvk.DestinationRule, Name: "a", Namespace: "b"}: {}})
		if _, _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if got, _, _ := c.Get(ep2); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.Clear(map[model.ConfigKey]struct{}{{Kind: gvk.DestinationRule, Name: "b", Namespace: "b"}: {}})
		if _, _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if _, _, f := c.Get(ep2); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})

	t.Run("clear all", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		addWithToken(c, ep1, any1)
		addWithToken(c, ep2, any2)

		c.ClearAll()
		if len(c.Keys()) != 0 {
			t.Fatalf("expected no keys, got: %v", c.Keys())
		}
		if _, _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if _, _, f := c.Get(ep2); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})

	t.Run("write without token", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		c.Add(ep1, 0, any1)
		if len(c.Keys()) != 0 {
			t.Fatalf("expected no keys, got: %v", c.Keys())
		}
	})

	t.Run("write with evicted token", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		addWithToken(c, ep1, any1)
		if len(c.Keys()) != 1 {
			t.Fatalf("expected 1 keys, got: %v", c.Keys())
		}
		_, tok, _ := c.Get(ep1)
		c.ClearAll()
		c.Add(ep1, tok, any1)
		if len(c.Keys()) != 0 {
			t.Fatalf("expected no keys, got: %v", c.Keys())
		}
	})
}
