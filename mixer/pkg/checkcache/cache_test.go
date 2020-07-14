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

package checkcache

import (
	"math/rand"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	mixerpb "istio.io/api/mixer/v1"
	"istio.io/pkg/attribute"
	"istio.io/pkg/log"
)

type getCase struct {
	name        string
	getBag      map[string]interface{}
	result      bool
	resultValue Value
}

func TestCache(t *testing.T) {
	cases := []struct {
		setBag   map[string]interface{}
		value    Value
		getCases []getCase
	}{
		{
			setBag: map[string]interface{}{
				"a": 1.0,
			},

			value: Value{
				StatusCode:    42,
				StatusMessage: "Forty Two",
				ValidUseCount: 32,
				Expiration:    time.Now().Add(time.Hour * -1000000),
				ReferencedAttributes: mixerpb.ReferencedAttributes{
					Words: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"},
					AttributeMatches: []mixerpb.ReferencedAttributes_AttributeMatch{
						{Name: -1, Condition: mixerpb.EXACT},
					},
				},
			},

			getCases: []getCase{
				{
					"out of date on set",
					map[string]interface{}{
						"a": 1.0,
					},
					false,
					Value{},
				},
			},
		},

		{
			setBag: map[string]interface{}{
				"a": 1.0,
				"b": 2.0,
			},

			value: Value{
				StatusCode:    42,
				StatusMessage: "Forty Two",
				ValidUseCount: 32,
				Expiration:    time.Now().Add(time.Hour * 10000),
				ReferencedAttributes: mixerpb.ReferencedAttributes{
					Words: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"},
					AttributeMatches: []mixerpb.ReferencedAttributes_AttributeMatch{
						{Name: -1, Condition: mixerpb.EXACT},
						{Name: -3, Condition: mixerpb.ABSENCE},
					},
				},
			},

			getCases: []getCase{
				{
					"match single exact",
					map[string]interface{}{
						"a": 1.0,
					},
					true,
					Value{
						StatusCode:    42,
						StatusMessage: "Forty Two",
						ValidUseCount: 32,
						ReferencedAttributes: mixerpb.ReferencedAttributes{
							Words: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"},
							AttributeMatches: []mixerpb.ReferencedAttributes_AttributeMatch{
								{Name: -1, Condition: mixerpb.EXACT},
								{Name: -3, Condition: mixerpb.ABSENCE},
							},
						},
					},
				},

				{
					"missing single exact",
					map[string]interface{}{
						"d": 1.0,
					},
					false,
					Value{},
				},
			},
		},

		{
			setBag: map[string]interface{}{
				"a": 1.0,
				"b": 2.0,
				"d": 3.0,
			},

			value: Value{
				StatusCode:    42,
				StatusMessage: "Forty Two",
				ValidUseCount: 32,
				Expiration:    time.Now().Add(time.Hour * 10000),
				ReferencedAttributes: mixerpb.ReferencedAttributes{
					Words: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"},
					AttributeMatches: []mixerpb.ReferencedAttributes_AttributeMatch{
						{Name: -1, Condition: mixerpb.EXACT},
						{Name: -3, Condition: mixerpb.ABSENCE},
					},
				},
			},

			getCases: []getCase{
				{
					"match single exact, reuse shape",
					map[string]interface{}{
						"a": 1.0,
					},
					true,
					Value{
						StatusCode:    42,
						StatusMessage: "Forty Two",
						ValidUseCount: 32,
						ReferencedAttributes: mixerpb.ReferencedAttributes{
							Words: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"},
							AttributeMatches: []mixerpb.ReferencedAttributes_AttributeMatch{
								{Name: -1, Condition: mixerpb.EXACT},
								{Name: -3, Condition: mixerpb.ABSENCE},
							},
						},
					},
				},
			},
		},
	}

	cache := New(10)

	for _, c := range cases {

		bag := attribute.GetMutableBagForTesting(c.setBag)
		cache.Set(bag, c.value)
		for _, gc := range c.getCases {
			t.Run(gc.name, func(t *testing.T) {

				bag = attribute.GetMutableBagForTesting(gc.getBag)
				value, ok := cache.Get(bag)
				if ok != gc.result {
					t.Errorf("Expecting %v, got %v", gc.result, ok)
				} else {
					if value.StatusCode != gc.resultValue.StatusCode {
						t.Errorf("Expecting status code of %v, got %v", gc.resultValue.StatusCode, value.StatusCode)
					}

					if value.StatusMessage != gc.resultValue.StatusMessage {
						t.Errorf("Expecting status message of %v, got %v", gc.resultValue.StatusMessage, value.StatusMessage)
					}

					if value.ValidUseCount != gc.resultValue.ValidUseCount {
						t.Errorf("Expecting use count of %v, got %v", gc.resultValue.ValidUseCount, value.ValidUseCount)
					}

					if !reflect.DeepEqual(value.ReferencedAttributes, gc.resultValue.ReferencedAttributes) {
						t.Errorf("Expecting %v ref attributes, got %v", gc.resultValue.ReferencedAttributes, value.ReferencedAttributes)
					}
				}
			})
		}
	}

	// a special case, try out an entry that is present in the underlying cache, but expired
	tb := map[string]interface{}{
		"a": 1.0,
	}

	if _, ok := cache.Get(attribute.GetMutableBagForTesting(tb)); !ok {
		t.Errorf("Expecting to find entry but didn't")
	}

	cache.getTime = func() time.Time { return time.Now().Add(time.Hour * 1000000) }

	if _, ok := cache.Get(attribute.GetMutableBagForTesting(tb)); ok {
		t.Errorf("Expecting to not find entry but did")
	}

	// make sure our metric callbacks don't crash...
	_, _ = prometheus.DefaultGatherer.Gather()

	if err := cache.Close(); err != nil {
		t.Errorf("Expecting success, got %v", err)
	}
}

const (
	benchmarkCacheCapacity = 4096
	benchmarkIterations    = 16000
	benchmarkBagsPerShape  = 256
)

type shapeData struct {
	matches []mixerpb.ReferencedAttributes_AttributeMatch
	bags    []attribute.Bag
}

func BenchmarkCache(b *testing.B) {
	shapes := []shapeData{
		{
			matches: []mixerpb.ReferencedAttributes_AttributeMatch{
				{Name: -1, Condition: mixerpb.EXACT},
				{Name: -2, Condition: mixerpb.EXACT},
				{Name: -3, Condition: mixerpb.EXACT},
			},
		},

		{
			matches: []mixerpb.ReferencedAttributes_AttributeMatch{
				{Name: -1, Condition: mixerpb.EXACT},
				{Name: -4, Condition: mixerpb.EXACT},
				{Name: -6, Condition: mixerpb.EXACT},
			},
		},

		{
			matches: []mixerpb.ReferencedAttributes_AttributeMatch{
				{Name: -4, Condition: mixerpb.EXACT},
				{Name: -5, Condition: mixerpb.EXACT},
				{Name: -6, Condition: mixerpb.EXACT},
			},
		},
	}

	for i := 0; i < benchmarkBagsPerShape; i++ {
		shapes[0].bags = append(shapes[0].bags, attribute.GetMutableBagForTesting(map[string]interface{}{
			"aaaaaaaaaaaaaaa": int64(i),
			"bbbbbbbbbbbbbbb": "a veeeeeeeeeeeeeeeeeeeeeerrrrrrryyyyyyyyyyyyyy looooooooooong string" + strconv.Itoa(i),
			"ccccccccccccccc": float64(i),
			"ddddddddddddddd": true,
			"eeeeeeeeeeeeeee": "eat your vegetables",
		}))

		shapes[1].bags = append(shapes[0].bags, attribute.GetMutableBagForTesting(map[string]interface{}{
			"aaaaaaaaaaaaaaa": int64(i),
			"bbbbbbbbbbbbbbb": "a veeeeeeeeeeeeeeeeeeeeeerrrrrrryyyyyyyyyyyyyy looooooooooong string" + strconv.Itoa(i),
			"ccccccccccccccc": float64(i),
			"ddddddddddddddd": true,
			"eeeeeeeeeeeeeee": "eat your vegetables",
			"fffffffffffffff": 3.14145692,
		}))

		shapes[2].bags = append(shapes[0].bags, attribute.GetMutableBagForTesting(map[string]interface{}{
			"ddddddddddddddd": int64(i),
			"eeeeeeeeeeeeeee": "eat your vegetables",
			"fffffffffffffff": float64(i),
		}))
	}

	exp := time.Now().Add(time.Hour * 100000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cc := New(benchmarkCacheCapacity)

		for j := 0; j < benchmarkIterations; j++ {
			shapeNum := rand.Int() % len(shapes)
			bagNum := rand.Int() % len(shapes[shapeNum].bags)

			ra := mixerpb.ReferencedAttributes{
				Words:            []string{"aaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbb", "ccccccccccccccc", "ddddddddddddddd", "eeeeeeeeeeeeeee", "fffffffffffffff"},
				AttributeMatches: shapes[shapeNum].matches,
			}

			bag := shapes[shapeNum].bags[bagNum]

			if _, ok := cc.Get(bag); !ok {
				value := Value{
					Expiration:           exp,
					ReferencedAttributes: ra,
				}
				cc.Set(bag, value)
			}
		}
	}
}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	o := log.DefaultOptions()
	o.SetOutputLevel(log.DefaultScopeName, log.DebugLevel)
	_ = log.Configure(o)
}
