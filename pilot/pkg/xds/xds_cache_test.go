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

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"go.uber.org/atomic"
	any "google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	any1 = &discovery.Resource{Resource: &any.Any{TypeUrl: "foo"}}
	any2 = &discovery.Resource{Resource: &any.Any{TypeUrl: "bar"}}
)

// TestXdsCacheToken is a regression test to ensure that we do not write
func TestXdsCacheToken(t *testing.T) {
	c := model.NewXdsCache()
	n := atomic.NewInt32(0)
	mkv := func(n int32) *discovery.Resource {
		return &discovery.Resource{Resource: &any.Any{TypeUrl: fmt.Sprint(n)}}
	}
	k := EndpointBuilder{clusterName: "key", service: &model.Service{
		Hostname: "foo.com",
	}}
	work := func(start time.Time, n int32) {
		v := mkv(n)
		time.Sleep(time.Millisecond * time.Duration(rand.Intn(100)))
		req := &model.PushRequest{Start: start}
		c.Add(k, req, v)
	}
	// 5 round of xds push
	for vals := 0; vals < 5; vals++ {
		c.ClearAll()
		n.Inc()
		start := time.Now()
		for i := 0; i < 5; i++ {
			go work(start, n.Load())
		}
		retry.UntilOrFail(t, func() bool {
			val, f := c.Get(k)
			return f && val.Resource.TypeUrl == fmt.Sprint(n.Load())
		})
		for i := 0; i < 5; i++ {
			val, f := c.Get(k)
			if !f {
				t.Fatalf("no cache found")
			}
			if f && val.Resource.TypeUrl != fmt.Sprint(n.Load()) {
				t.Fatalf("got bad write: %v", val.Resource.TypeUrl)
			}
			time.Sleep(time.Millisecond * time.Duration(rand.Intn(20)))
		}
	}
}

func TestXdsCache(t *testing.T) {
	ep1 := EndpointBuilder{
		clusterName: "outbound|1||foo.com",
		service: &model.Service{
			Hostname: "foo.com",
		},
	}
	ep2 := EndpointBuilder{
		clusterName: "outbound|2||foo.com",
		service: &model.Service{
			Hostname: "foo.com",
		},
	}
	t.Run("simple", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		c.Add(ep1, &model.PushRequest{Start: time.Now()}, any1)
		if !reflect.DeepEqual(c.Keys(), []string{ep1.Key()}) {
			t.Fatalf("unexpected keys: %v, want %v", c.Keys(), ep1.Key())
		}
		if got, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}
		c.Add(ep1, &model.PushRequest{Start: time.Now()}, any2)
		if got, _ := c.Get(ep1); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}

		c.Clear(map[model.ConfigKey]struct{}{{Kind: kind.ServiceEntry, Name: "foo.com"}: {}})
		if _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})

	t.Run("multiple hostnames", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		start := time.Now()
		c.Add(ep1, &model.PushRequest{Start: start}, any1)
		c.Add(ep2, &model.PushRequest{Start: start}, any2)

		if got, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}
		if got, _ := c.Get(ep2); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.Clear(map[model.ConfigKey]struct{}{{Kind: kind.ServiceEntry, Name: "foo.com"}: {}})
		if _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if _, f := c.Get(ep2); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})

	t.Run("multiple destinationRules", func(t *testing.T) {
		c := model.NewLenientXdsCache()

		ep1 := ep1
		ep1.destinationRule = model.ConvertConsolidatedDestRule(&config.Config{Meta: config.Meta{Name: "a", Namespace: "b"}})
		ep2 := ep2
		ep2.destinationRule = model.ConvertConsolidatedDestRule(&config.Config{Meta: config.Meta{Name: "b", Namespace: "b"}})
		start := time.Now()
		c.Add(ep1, &model.PushRequest{Start: start}, any1)
		c.Add(ep2, &model.PushRequest{Start: start}, any2)
		if got, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}
		if got, _ := c.Get(ep2); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.Clear(map[model.ConfigKey]struct{}{{Kind: kind.DestinationRule, Name: "a", Namespace: "b"}: {}})
		if _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if got, _ := c.Get(ep2); got != any2 {
			t.Fatalf("unexpected result: %v, want %v", got, any2)
		}
		c.Clear(map[model.ConfigKey]struct{}{{Kind: kind.DestinationRule, Name: "b", Namespace: "b"}: {}})
		if _, f := c.Get(ep1); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
		if _, f := c.Get(ep2); f {
			t.Fatalf("unexpected result, found key when not expected: %v", c.Keys())
		}
	})

	t.Run("clear all", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		start := time.Now()
		c.Add(ep1, &model.PushRequest{Start: start}, any1)
		c.Add(ep2, &model.PushRequest{Start: start}, any2)

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

	t.Run("dependent type clears all", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		start := time.Now()
		c.Add(ep1, &model.PushRequest{Start: start}, any1)
		c.Add(ep2, &model.PushRequest{Start: start}, any2)

		c.Clear(map[model.ConfigKey]struct{}{{Kind: kind.PeerAuthentication}: {}})
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

	t.Run("write without token does nothing", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		c.Add(ep1, &model.PushRequest{}, any1)
		if got, f := c.Get(ep1); f {
			t.Fatalf("unexpected result: %v, want none", got)
		}
	})

	t.Run("write with evicted token", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		t1 := time.Now()
		t2 := t1.Add(1 * time.Nanosecond)
		c.Add(ep1, &model.PushRequest{Start: t2}, any1)
		c.Add(ep1, &model.PushRequest{Start: t1}, any2)
		if len(c.Keys()) != 1 {
			t.Fatalf("expected 1 keys, got: %v", c.Keys())
		}
		if got, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}
	})

	t.Run("write with expired token", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		t1 := time.Now()
		t2 := t1.Add(-1 * time.Nanosecond)

		c.Add(ep1, &model.PushRequest{Start: t1}, any1)
		c.ClearAll()
		// prevented, this is stale token
		c.Add(ep1, &model.PushRequest{Start: t2}, any2)
		if got, _ := c.Get(ep1); got != nil {
			t.Fatalf("expected no cache, but got %v", got)
		}
	})

	t.Run("disallow write with stale token after clear", func(t *testing.T) {
		c := model.NewLenientXdsCache()
		t1 := time.Now()

		c.Add(ep1, &model.PushRequest{Start: t1}, any1)
		c.ClearAll()
		// prevented, this can be stale data after `disallowCacheSameToken`
		c.Add(ep1, &model.PushRequest{Start: t1}, any2)
		if got, _ := c.Get(ep1); got != nil {
			t.Fatalf("expected no cache, but got %v", got)
		}

		// cache with newer token
		c.Add(ep1, &model.PushRequest{Start: time.Now()}, any1)
		if got, _ := c.Get(ep1); got != any1 {
			t.Fatalf("unexpected result: %v, want %v", got, any1)
		}
	})
}
