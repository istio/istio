//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package common

import (
	"fmt"
	"testing"
)

func TestMapEquals(t *testing.T) {
	tests := []struct {
		m1      map[string]string
		m2      map[string]string
		exclude []string
		expect  bool
	}{
		{
			m1:      nil,
			m2:      nil,
			exclude: nil,
			expect:  true,
		},
		{
			m1:      nil,
			m2:      map[string]string{},
			exclude: nil,
			expect:  false,
		},
		{
			m1:      map[string]string{},
			m2:      nil,
			exclude: nil,
			expect:  false,
		},
		{
			m1: map[string]string{
				"foo": "bar",
				"baz": "boo",
			},
			m2: map[string]string{
				"foo": "bar",
				"baz": "boo",
			},
			exclude: nil,
			expect:  true,
		},
		{
			m1: map[string]string{
				"foo": "notbar",
				"baz": "boo",
			},
			m2: map[string]string{
				"foo": "bar",
				"baz": "boo",
			},
			exclude: nil,
			expect:  false,
		},
		{
			m1: map[string]string{
				"foo": "bar",
				"baz": "boo",
			},
			m2: map[string]string{
				"foo": "bar",
			},
			exclude: nil,
			expect:  false,
		},
		{
			m1: map[string]string{
				"foo": "bar",
			},
			m2: map[string]string{
				"foo": "bar",
				"baz": "boo",
			},
			exclude: nil,
			expect:  false,
		},
		{
			m1: map[string]string{
				"foo": "bar",
			},
			m2: map[string]string{
				"foo": "bar",
				"baz": "boo",
			},
			exclude: []string{"baz"},
			expect:  true,
		},
		{
			m1: map[string]string{
				"foo": "bar",
				"baz": "boo",
			},
			m2: map[string]string{
				"foo": "bar",
			},
			exclude: []string{"baz"},
			expect:  true,
		},
		{
			m1: map[string]string{
				"foo": "bar",
				"boo": "far",
			},
			m2: map[string]string{
				"foo": "bar",
				"boo": "far",
				"baz": "boo",
			},
			exclude: []string{"baz"},
			expect:  true,
		},
		{
			m1: map[string]string{
				"foo": "bar",
				"boo": "far",
				"baz": "boo",
			},
			m2: map[string]string{
				"foo": "bar",
				"boo": "far",
			},
			exclude: []string{"baz"},
			expect:  true,
		},
		{
			m1: map[string]string{
				"foo": "foo",
				"boo": "far",
				"baz": "boo",
			},
			m2: map[string]string{
				"foo": "otherfoo",
				"boo": "far",
			},
			exclude: []string{"foo", "baz"},
			expect:  true,
		},
	}

	for i, tst := range tests {
		t.Run(fmt.Sprintf("%d", i), func(tt *testing.T) {
			actual := MapEquals(tst.m1, tst.m2, tst.exclude...)

			if actual != tst.expect {
				tt.Fatalf("Unexpected result: got:'%v', wanted:'%v'", actual, tst.expect)
			}
		})
	}
}
