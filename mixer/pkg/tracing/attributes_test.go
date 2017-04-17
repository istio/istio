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

package tracing

import (
	"context"
	"errors"
	"testing"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/mixer/pkg/attribute"
)

var (
	attrs = &mixerpb.Attributes{
		Dictionary:       map[int32]string{1: "N1", 2: "N2", 3: "N3", 4: "N4", 5: "N5", 6: "N6", 7: "N7", 8: "N8", 9: "N11", 10: "N12"},
		StringAttributes: map[int32]string{1: "1", 2: "2"},
		Int64Attributes:  map[int32]int64{3: 3, 4: 4},
		DoubleAttributes: map[int32]float64{5: 5.0, 6: 6.0},
		BoolAttributes:   map[int32]bool{7: true, 8: false},
		BytesAttributes:  map[int32][]uint8{9: {9}, 10: {10}},
	}
)

func TestContext(t *testing.T) {
	bag, err := attribute.NewManager().NewTracker().ApplyProto(attrs)
	if err != nil {
		t.Errorf("Failed to construct bag: %v", err)
	}

	if c := FromContext(context.Background()); c != nil {
		t.Errorf("FromContext(context.Background()) = %v; wanted nil", c)
	}

	c := NewCarrier(bag)

	ctx := context.Background()
	ctx = NewContext(ctx, c)
	if cc := FromContext(ctx); cc != c {
		t.Errorf("FromContext(ctx) = %v; wanted %v; context: %v", cc, c, ctx)
	}
}

func TestSet(t *testing.T) {
	bag, err := attribute.NewManager().NewTracker().ApplyProto(attrs)
	if err != nil {
		t.Errorf("Failed to construct bag: %v", err)
	}

	cases := []struct {
		key string
		val string
	}{
		{"a", "b"},
		{"foo", "bar"},
		{"spanID", "1235"},
	}
	for _, c := range cases {
		cr := NewCarrier(bag)
		cr.Set(c.key, c.val)
		if val, ok := cr.mb.Get(prefix + c.key); !ok || val.(string) != c.val {
			t.Errorf("carrier.Set(%s, %s); bag.String(%s) = %s; wanted %s", c.key, c.val, prefix+c.key, val, c.val)
		}
	}
}

func TestForeachKey(t *testing.T) {
	bag, err := attribute.NewManager().NewTracker().ApplyProto(attrs)
	if err != nil {
		t.Errorf("Failed to construct bag: %v", err)
	}

	cases := []struct {
		data map[string]string
	}{
		{map[string]string{"a": "b", "c": "d"}},
	}
	for _, c := range cases {
		b := bag.Child()
		for k, v := range c.data {
			b.Set(prefix+k, v)
		}

		err := NewCarrier(b).ForeachKey(func(key, val string) error {
			if dval, found := c.data[key]; !found || dval != val {
				t.Errorf("ForeachKey(func(%s, %s)); wanted func(%s, %s)", key, val, key, dval)
			}
			return nil
		})
		if err != nil {
			t.Errorf("ForeachKey failed with unexpected err: %v", err)
		}
	}
}

func TestForeachKey_PropagatesErr(t *testing.T) {
	bag, err := attribute.NewManager().NewTracker().ApplyProto(attrs)
	if err != nil {
		t.Errorf("Failed to construct bag: %v", err)
	}

	c := NewCarrier(bag)
	c.Set("k", "v")

	err = errors.New("expected")
	propErr := c.ForeachKey(func(key, val string) error {
		return err
	})

	if propErr != err {
		t.Errorf("ForeachKey(...) = %v; wanted err %v", propErr, err)
	}
}
