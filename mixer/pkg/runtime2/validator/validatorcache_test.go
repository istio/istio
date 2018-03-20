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

package validator

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"

	cpb "istio.io/api/policy/v1beta1"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/pkg/cache"
)

const expirationForTest = 10 * time.Millisecond
const watchFlushDurationForTest = time.Millisecond

type validatorCacheTestController struct {
	c       *validatorCache
	s       store.Store
	m       *storetest.Memstore
	donec   chan struct{}
	updatec chan struct{}
}

func newValidatorCacheForTest() (*validatorCacheTestController, error) {
	m := storetest.NewMemstore()
	s := store.WithBackend(m)
	if err := s.Init(map[string]proto.Message{config.RulesKind: &cpb.Rule{}, config.AttributeManifestKind: &cpb.AttributeManifest{}}); err != nil {
		return nil, err
	}
	c := &validatorCache{
		c:          cache.NewTTL(expirationForTest, expirationForTest*2),
		configData: map[store.Key]proto.Message{},
	}
	wch, err := s.Watch()
	if err != nil {
		return nil, err
	}
	controller := &validatorCacheTestController{
		c:       c,
		s:       s,
		m:       m,
		donec:   make(chan struct{}),
		updatec: make(chan struct{}, 100),
	}
	go store.WatchChanges(wch, controller.donec, watchFlushDurationForTest, controller.applyChanges)
	return controller, nil
}

func (c *validatorCacheTestController) stop() {
	close(c.donec)
	c.s.Stop()
}

func (c *validatorCacheTestController) applyChanges(evs []*store.Event) {
	c.c.applyChanges(evs)
	select {
	case c.updatec <- struct{}{}:
	default:
	}
}

func (c *validatorCacheTestController) invalidateCache(key store.Key) {
	// To run the test deterministically, this does not sleep to the key goes away
	// from the cache, instead removes the cached data explicitly.
	c.c.c.Remove(key)
}

func (c *validatorCacheTestController) assertListKeys(t *testing.T, want ...store.Key) {
	t.Helper()
	if want == nil {
		want = []store.Key{}
	}
	sort.Slice(want, func(i, j int) bool {
		return want[i].String() < want[j].String()
	})
	got := make([]store.Key, 0, len(want))
	c.c.forEach(func(key store.Key, obj proto.Message) {
		got = append(got, key)
	})
	sort.Slice(got, func(i, j int) bool {
		return got[i].String() < got[j].String()
	})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Got %+v, Want %+v", got, want)
	}
}

func (c *validatorCacheTestController) assertExpectedData(t *testing.T, key store.Key, want proto.Message) {
	t.Helper()
	got, ok := c.c.get(key)
	if ok {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Got %+v, Want %+v", got, want)
		}
	} else if want != nil {
		t.Errorf("Doesn't exist, Want %+v", want)
	}
}

func TestValidatorCache(t *testing.T) {
	c, err := newValidatorCacheForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer c.stop()
	c.assertListKeys(t)
	k1 := store.Key{Kind: config.RulesKind, Name: "foo", Namespace: "ns"}
	r1 := &store.BackEndResource{Kind: k1.Kind, Metadata: store.ResourceMeta{Name: k1.Name, Namespace: k1.Namespace}, Spec: map[string]interface{}{"match": "foo"}}
	c.m.Put(r1)
	<-c.updatec
	c.assertListKeys(t, k1)
	c.assertExpectedData(t, k1, &cpb.Rule{Match: "foo"})

	c.c.putCache(&store.Event{Key: k1, Type: store.Update, Value: &store.Resource{Spec: &cpb.Rule{Match: "bar"}}})
	c.assertExpectedData(t, k1, &cpb.Rule{Match: "bar"})
	c.invalidateCache(k1)
	c.assertExpectedData(t, k1, &cpb.Rule{Match: "foo"})

	c.c.putCache(&store.Event{Key: k1, Type: store.Update, Value: &store.Resource{Spec: &cpb.Rule{Match: "bar"}}})
	c.assertExpectedData(t, k1, &cpb.Rule{Match: "bar"})
	r1.Spec = map[string]interface{}{"match": "bar"}
	c.m.Put(r1)
	<-c.updatec
	c.invalidateCache(k1)
	c.assertExpectedData(t, k1, &cpb.Rule{Match: "bar"})

	c.c.putCache(&store.Event{Key: k1, Type: store.Delete})
	c.assertExpectedData(t, k1, nil)
	c.invalidateCache(k1)
	c.assertExpectedData(t, k1, &cpb.Rule{Match: "bar"})

	c.c.putCache(&store.Event{Key: k1, Type: store.Delete})
	c.assertExpectedData(t, k1, nil)
	c.m.Delete(k1)
	<-c.updatec
	c.invalidateCache(k1)
	c.assertExpectedData(t, k1, nil)
}

func TestValidatorCacheDoubleEdits(t *testing.T) {
	spec1 := &cpb.Rule{Match: "spec1"}
	spec2 := &cpb.Rule{Match: "spec2"}
	base := &cpb.Rule{Match: "base"}
	k1 := store.Key{Kind: config.RulesKind, Name: "foo", Namespace: "ns"}
	meta1 := store.ResourceMeta{Name: k1.Name, Namespace: k1.Namespace}
	setWait := func(tt *testing.T, c *validatorCacheTestController, data proto.Message) {
		tt.Helper()
		c.c.putCache(&store.Event{Key: k1, Type: store.Update, Value: &store.Resource{Spec: data}})
		c.assertExpectedData(tt, k1, data)
	}
	delWait := func(tt *testing.T, c *validatorCacheTestController) {
		// tt.Helper()
		c.c.putCache(&store.Event{Key: k1, Type: store.Delete})
		c.assertExpectedData(tt, k1, nil)
	}

	for _, cc := range []struct {
		title   string
		prepare func(tt *testing.T, c *validatorCacheTestController)
		op      func(tt *testing.T, m *storetest.Memstore)
		want    proto.Message
	}{
		{
			"put-put-1",
			func(tt *testing.T, c *validatorCacheTestController) {
				setWait(tt, c, spec1)
				setWait(tt, c, spec2)
			},
			nil,
			base,
		},
		{
			"put-put-2",
			func(tt *testing.T, c *validatorCacheTestController) {
				setWait(tt, c, spec1)
				setWait(tt, c, spec2)
			},
			func(tt *testing.T, m *storetest.Memstore) {
				m.Put(&store.BackEndResource{Kind: k1.Kind, Metadata: meta1, Spec: map[string]interface{}{
					"match": "spec2",
				}})
			},
			spec2,
		},
		{
			"put-put-3",
			func(tt *testing.T, c *validatorCacheTestController) {
				setWait(tt, c, spec1)
				setWait(tt, c, spec2)
			},
			func(tt *testing.T, m *storetest.Memstore) {
				m.Put(&store.BackEndResource{Kind: k1.Kind, Metadata: meta1, Spec: map[string]interface{}{
					"match": "spec1",
				}})
			},
			spec1,
		},
		{
			"put-delete-1",
			func(tt *testing.T, c *validatorCacheTestController) {
				setWait(tt, c, spec1)
				delWait(tt, c)
			},
			nil,
			base,
		},
		{
			"put-delete-2",
			func(tt *testing.T, c *validatorCacheTestController) {
				setWait(tt, c, spec1)
				delWait(tt, c)
			},
			func(tt *testing.T, m *storetest.Memstore) {
				m.Delete(k1)
			},
			nil,
		},
		{
			"put-delete-3",
			func(tt *testing.T, c *validatorCacheTestController) {
				setWait(tt, c, spec1)
				delWait(tt, c)
			},
			func(tt *testing.T, m *storetest.Memstore) {
				m.Put(&store.BackEndResource{Kind: k1.Kind, Metadata: meta1, Spec: map[string]interface{}{
					"match": "spec1",
				}})
			},
			spec1,
		},
		{
			"delete-put-1",
			func(tt *testing.T, c *validatorCacheTestController) {
				delWait(tt, c)
				setWait(tt, c, spec1)
			},
			nil,
			base,
		},
		{
			"delete-put-2",
			func(tt *testing.T, c *validatorCacheTestController) {
				delWait(tt, c)
				setWait(tt, c, spec1)
			},
			func(tt *testing.T, m *storetest.Memstore) {
				m.Delete(k1)
			},
			nil,
		},
		{
			"delete-put-3",
			func(tt *testing.T, c *validatorCacheTestController) {
				delWait(tt, c)
				setWait(tt, c, spec1)
			},
			func(tt *testing.T, m *storetest.Memstore) {
				m.Put(&store.BackEndResource{Kind: k1.Kind, Metadata: meta1, Spec: map[string]interface{}{
					"match": "spec1",
				}})
			},
			spec1,
		},
	} {
		t.Run(cc.title, func(tt *testing.T) {
			c, err := newValidatorCacheForTest()
			if err != nil {
				t.Fatal(err)
			}
			defer c.stop()

			r1 := &store.BackEndResource{Kind: k1.Kind, Metadata: meta1, Spec: map[string]interface{}{"match": "base"}}
			c.m.Put(r1)
			<-c.updatec
			c.assertExpectedData(tt, k1, base)

			cc.prepare(tt, c)
			if cc.op != nil {
				cc.op(tt, c.m)
				<-c.updatec
			}
			c.invalidateCache(k1)
			c.assertExpectedData(tt, k1, cc.want)
		})
	}
}
