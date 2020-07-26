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
	"testing"
	"time"

	mixerpb "istio.io/api/mixer/v1"
	attr "istio.io/istio/mixer/pkg/attribute"
	"istio.io/pkg/attribute"
	"istio.io/pkg/log"
)

type attrCase struct {
	name         string
	bag          map[string]interface{}
	checkPresent bool
	checkAbsent  bool
}

func TestKeyShape(t *testing.T) {
	allKeys := make(map[string]struct{})
	globalWords := attr.GlobalList()

	cases := []struct {
		ra    mixerpb.ReferencedAttributes
		attrs []attrCase
	}{
		{
			ra: mixerpb.ReferencedAttributes{
				Words:            nil,
				AttributeMatches: []mixerpb.ReferencedAttributes_AttributeMatch{},
			},

			attrs: []attrCase{
				{
					"empty ra, empty bag",
					map[string]interface{}{},
					true,
					true,
				},

				{
					"empty ra, bag with stuff in it",
					map[string]interface{}{
						"a": "b",
					},
					true,
					true,
				},
			},
		},

		{
			ra: mixerpb.ReferencedAttributes{
				Words: nil,
				AttributeMatches: []mixerpb.ReferencedAttributes_AttributeMatch{
					{Name: 0, Condition: mixerpb.EXACT},
				},
			},

			attrs: []attrCase{
				{
					"ra with single exact entry, empty bag",
					map[string]interface{}{},
					false,
					true,
				},

				{
					"ra with single exact entry, bag with matching entry",
					map[string]interface{}{
						globalWords[0]: "b",
					},
					true,
					true,
				},

				{
					"ra with single exact entry, bag with non-matching entry",
					map[string]interface{}{
						globalWords[1]: "b",
					},
					false,
					true,
				},

				{
					"ra with single exact entry, bag with matching entries and more",
					map[string]interface{}{
						globalWords[0]: "0",
						"b":            int64(0),
						"c":            0.0,
						"d":            true,
						"e":            false,
						"f":            []byte{0, 1, 2},
						"g":            time.Now(),
						"h":            time.Second * 10,
						"i":            attribute.WrapStringMap(map[string]string{"j": "k"}),
					},
					true,
					true,
				},
			},
		},

		{
			ra: mixerpb.ReferencedAttributes{
				Words: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"},
				AttributeMatches: []mixerpb.ReferencedAttributes_AttributeMatch{
					{Name: -1, Condition: mixerpb.EXACT},
					{Name: -2, Condition: mixerpb.EXACT},
					{Name: -3, Condition: mixerpb.EXACT},
					{Name: -4, Condition: mixerpb.EXACT},
					{Name: -5, Condition: mixerpb.EXACT},
					{Name: -6, Condition: mixerpb.EXACT},
					{Name: -7, Condition: mixerpb.EXACT},
					{Name: -9, Condition: mixerpb.EXACT, MapKey: -10},
					{Name: -9, Condition: mixerpb.EXACT, MapKey: -11},
					{Name: -8, Condition: mixerpb.EXACT},
				},
			},

			attrs: []attrCase{
				{
					"ra with many exact entries, empty bag",
					map[string]interface{}{},
					false,
					true,
				},

				{
					"ra with many exact entries, bag with one matching entry, missing many",
					map[string]interface{}{
						"a": "b",
					},
					false,
					true,
				},

				{
					"ra with many exact entries, bag matching all",
					map[string]interface{}{
						"a": "0",
						"b": int64(0),
						"c": 0.0,
						"d": true,
						"e": false,
						"f": []byte{0, 1, 2},
						"g": time.Now(),
						"h": time.Second * 10,
						"i": attribute.WrapStringMap(map[string]string{"j": "j", "k": "k"}),
					},
					true,
					true,
				},

				{
					"ra with many exact entries, bag matching all, plus extra",
					map[string]interface{}{
						"a": "0",
						"b": int64(0),
						"c": 0.0,
						"d": true,
						"e": false,
						"f": []byte{0, 1, 2},
						"g": time.Now(),
						"h": 10 * time.Second,
						"i": attribute.WrapStringMap(map[string]string{"j": "j", "k": "k", "l": "l"}),
						"j": "k",
					},
					true,
					true,
				},

				{
					"ra with many exact entries, bag matching all, except map key",
					map[string]interface{}{
						"a": "0",
						"b": int64(0),
						"c": 0.0,
						"d": true,
						"e": false,
						"f": []byte{0, 1, 2},
						"g": time.Now(),
						"h": 10 * time.Second,
						"i": attribute.WrapStringMap(map[string]string{"X": "k", "l": "m"}),
					},
					false,
					true,
				},
			},
		},

		{
			ra: mixerpb.ReferencedAttributes{
				Words: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"},
				AttributeMatches: []mixerpb.ReferencedAttributes_AttributeMatch{
					{Name: -1, Condition: mixerpb.ABSENCE},
					{Name: -2, Condition: mixerpb.ABSENCE},
					{Name: -3, Condition: mixerpb.ABSENCE},
					{Name: -4, Condition: mixerpb.ABSENCE},
					{Name: -5, Condition: mixerpb.ABSENCE},
					{Name: -6, Condition: mixerpb.ABSENCE},
					{Name: -7, Condition: mixerpb.ABSENCE},
					{Name: -9, Condition: mixerpb.ABSENCE, MapKey: -10},
					{Name: -9, Condition: mixerpb.ABSENCE, MapKey: -11},
					{Name: -8, Condition: mixerpb.ABSENCE},
				},
			},

			attrs: []attrCase{
				{
					"ra with many absent entries, empty bag",
					map[string]interface{}{},
					true,
					true,
				},

				{
					"ra with many absent entries, three unrelated entries in bag",
					map[string]interface{}{
						"x": "y",
						"z": attribute.WrapStringMap(map[string]string{"Y": "Y"}),
						"i": attribute.WrapStringMap(map[string]string{"Y": "Y"}),
					},
					true,
					true,
				},

				{
					"ra with many absent entries, two unrelated entries in bag and an absent entry present",
					map[string]interface{}{
						"x": "y",
						"z": attribute.WrapStringMap(map[string]string{"Y": "Y"}),
						"a": int64(0),
					},
					true,
					false,
				},

				{
					"ra with many absent entries, absent key in string map",
					map[string]interface{}{
						"x": "y",
						"z": attribute.WrapStringMap(map[string]string{"Y": "Y"}),
						"i": attribute.WrapStringMap(map[string]string{"j": "j", "k": "k"}),
					},
					true,
					false,
				},
			},
		},

		{
			ra: mixerpb.ReferencedAttributes{
				Words: nil,
				AttributeMatches: []mixerpb.ReferencedAttributes_AttributeMatch{
					{Name: -1, Condition: mixerpb.ABSENCE},
					{Name: 100000, Condition: mixerpb.ABSENCE},
				},
			},

			attrs: []attrCase{
				{
					"ra with bogus dictionary indices, empty bag",
					map[string]interface{}{},
					true,
					true,
				},

				{
					"ra with bogus dictionary indices, bag with single entry",
					map[string]interface{}{
						"a": "b",
					},
					true,
					true,
				},
			},
		},
	}

	for _, c := range cases {
		shape := newKeyShape(c.ra, globalWords)

		for _, ac := range c.attrs {
			t.Run(ac.name, func(t *testing.T) {
				bag := attribute.GetMutableBagForTesting(ac.bag)

				if ok := shape.checkAbsentAttrs(bag); ok != ac.checkAbsent {
					t.Errorf("Expecting checkAbsent %v, got %v", ac.checkAbsent, ok)
				}

				if ok := shape.checkPresentAttrs(bag); ok != ac.checkPresent {
					t.Errorf("Expecting checkPresent %v, got %v", ac.checkPresent, ok)
				}

				if ok := shape.isCompatible(bag); ok != ac.checkAbsent && ac.checkPresent {
					t.Errorf("Expecting %v, got %v", ac.checkAbsent && ac.checkPresent, ok)
				}

				if shape.isCompatible(bag) {
					key := shape.makeKey(bag)
					if _, ok := allKeys[key]; ok {
						t.Errorf("Expecting all jeys to be different, found %v a second time", key)
					}
				}
			})
		}
	}
}

func init() {
	// bump up the log level so log-only logic runs during the tests, for correctness and coverage.
	o := log.DefaultOptions()
	o.SetOutputLevel(log.DefaultScopeName, log.DebugLevel)
	_ = log.Configure(o)
}
