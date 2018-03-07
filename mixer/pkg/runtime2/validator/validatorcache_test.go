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

	cpb "istio.io/api/mixer/v1/config"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/pkg/cache"
)

const expirationForTest = 10 * time.Millisecond
const watchFlushDurationForTest = time.Millisecond

func newValidatorCacheForTest(name string) (*validatorCache, store.Store, *storetest.Memstore, chan struct{}, error) {
	m := storetest.NewMemstore()
	s := store.WithBackend(m)
	if err := s.Init(map[string]proto.Message{config.RulesKind: &cpb.Rule{}, config.AttributeManifestKind: &cpb.AttributeManifest{}}); err != nil {
		return nil, nil, nil, nil, err
	}
	c := &validatorCache{
		c:          cache.NewTTL(expirationForTest, expirationForTest*2),
		configData: map[store.Key]proto.Message{},
	}
	wch, err := s.Watch()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	donec := make(chan struct{})
	go watchChanges(donec, wch, watchFlushDurationForTest, c.applyChanges)
	return c, s, m, donec, nil
}

func assertListKeys(t *testing.T, c *validatorCache, want ...store.Key) {
	t.Helper()
	if want == nil {
		want = []store.Key{}
	}
	sort.Slice(want, func(i, j int) bool {
		return want[i].String() < want[j].String()
	})
	got := make([]store.Key, 0, len(want))
	c.forEach(func(key store.Key, obj proto.Message) {
		got = append(got, key)
	})
	sort.Slice(got, func(i, j int) bool {
		return got[i].String() < got[j].String()
	})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Got %+v, Want %+v", got, want)
	}
}

func assertExpectedData(t *testing.T, c *validatorCache, key store.Key, want proto.Message) {
	t.Helper()
	got, ok := c.get(key)
	if ok {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Got %+v, Want %+v", got, want)
		}
	} else if want != nil {
		t.Errorf("Doesn't exist, Want %+v", want)
	}
}

func TestValidatorCache(t *testing.T) {
	c, s, m, donec, err := newValidatorCacheForTest(t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer close(donec)
	defer s.Stop()
	assertListKeys(t, c)
	k1 := store.Key{Kind: config.RulesKind, Name: "foo", Namespace: "ns"}
	r1 := &store.BackEndResource{Kind: k1.Kind, Metadata: store.ResourceMeta{Name: k1.Name, Namespace: k1.Namespace}, Spec: map[string]interface{}{"match": "foo"}}
	m.Put(r1)
	time.Sleep(watchFlushDurationForTest * 2)
	assertListKeys(t, c, k1)
	assertExpectedData(t, c, k1, &cpb.Rule{Match: "foo"})

	c.putCache(&store.Event{Key: k1, Type: store.Update, Value: &store.Resource{Spec: &cpb.Rule{Match: "bar"}}})
	time.Sleep(2 * time.Millisecond)
	assertExpectedData(t, c, k1, &cpb.Rule{Match: "bar"})
	time.Sleep(expirationForTest * 2)
	assertExpectedData(t, c, k1, &cpb.Rule{Match: "foo"})

	c.putCache(&store.Event{Key: k1, Type: store.Update, Value: &store.Resource{Spec: &cpb.Rule{Match: "bar"}}})
	time.Sleep(2 * time.Millisecond)
	assertExpectedData(t, c, k1, &cpb.Rule{Match: "bar"})
	r1.Spec = map[string]interface{}{"match": "bar"}
	m.Put(r1)
	time.Sleep(expirationForTest * 2)
	assertExpectedData(t, c, k1, &cpb.Rule{Match: "bar"})

	c.putCache(&store.Event{Key: k1, Type: store.Delete})
	time.Sleep(2 * time.Millisecond)
	assertExpectedData(t, c, k1, nil)
	time.Sleep(expirationForTest * 2)
	assertExpectedData(t, c, k1, &cpb.Rule{Match: "bar"})

	c.putCache(&store.Event{Key: k1, Type: store.Delete})
	time.Sleep(2 * time.Millisecond)
	assertExpectedData(t, c, k1, nil)
	m.Delete(k1)
	time.Sleep(expirationForTest * 2)
	assertExpectedData(t, c, k1, nil)
}

func TestValidatorCacheDoubleEdits(t *testing.T) {
	spec1 := &cpb.Rule{Match: "spec1"}
	spec2 := &cpb.Rule{Match: "spec2"}
	base := &cpb.Rule{Match: "base"}
	k1 := store.Key{Kind: config.RulesKind, Name: "foo", Namespace: "ns"}
	meta1 := store.ResourceMeta{Name: k1.Name, Namespace: k1.Namespace}
	setWait := func(tt *testing.T, c *validatorCache, data proto.Message) {
		// tt.Helper()
		c.putCache(&store.Event{Key: k1, Type: store.Update, Value: &store.Resource{Spec: data}})
		time.Sleep(2 * time.Millisecond)
		assertExpectedData(tt, c, k1, data)
	}
	delWait := func(tt *testing.T, c *validatorCache) {
		// tt.Helper()
		c.putCache(&store.Event{Key: k1, Type: store.Delete})
		time.Sleep(2 * time.Millisecond)
		assertExpectedData(tt, c, k1, nil)
	}

	for _, cc := range []struct {
		title   string
		prepare func(tt *testing.T, c *validatorCache)
		op      func(tt *testing.T, m *storetest.Memstore)
		want    proto.Message
	}{
		{
			"put-put-1",
			func(tt *testing.T, c *validatorCache) {
				setWait(tt, c, spec1)
				setWait(tt, c, spec2)
			},
			nil,
			base,
		},
		{
			"put-put-2",
			func(tt *testing.T, c *validatorCache) {
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
			func(tt *testing.T, c *validatorCache) {
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
			func(tt *testing.T, c *validatorCache) {
				setWait(tt, c, spec1)
				delWait(tt, c)
			},
			nil,
			base,
		},
		{
			"put-delete-2",
			func(tt *testing.T, c *validatorCache) {
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
			func(tt *testing.T, c *validatorCache) {
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
			func(tt *testing.T, c *validatorCache) {
				delWait(tt, c)
				setWait(tt, c, spec1)
			},
			nil,
			base,
		},
		{
			"delete-put-2",
			func(tt *testing.T, c *validatorCache) {
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
			func(tt *testing.T, c *validatorCache) {
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
			c, s, m, donec, err := newValidatorCacheForTest(t.Name())
			if err != nil {
				t.Fatal(err)
			}
			defer close(donec)
			defer s.Stop()

			r1 := &store.BackEndResource{Kind: k1.Kind, Metadata: meta1, Spec: map[string]interface{}{"match": "base"}}
			m.Put(r1)
			time.Sleep(watchFlushDurationForTest * 2)
			assertExpectedData(tt, c, k1, base)

			cc.prepare(tt, c)
			if cc.op != nil {
				cc.op(tt, m)
			}
			time.Sleep(expirationForTest * 2)
			assertExpectedData(tt, c, k1, cc.want)
		})
	}
}
