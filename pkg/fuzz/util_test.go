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

package fuzz

import (
	"reflect"
	"testing"
	"time"

	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test/util/assert"
)

type demo struct {
	String  string
	priv    string // private filed
	Int     int
	Slice   []int
	Map     map[int]int
	Bool    bool
	Pointer *demo
}

func TestMutateStruct(t *testing.T) {
	cases := []struct {
		name  string
		input demo
		want  demo
	}{
		{
			name:  "empty input",
			input: demo{},
			want: demo{
				String:  "mutated",
				priv:    "mutated",
				Int:     1,
				Slice:   nil,
				Map:     nil,
				Bool:    true,
				Pointer: nil,
			},
		},
		{
			name: "zero value",
			input: demo{
				String: "",
				priv:   "",
				Int:    0,
				Slice:  []int{},
				Map:    map[int]int{},
				Bool:   false,
				Pointer: &demo{
					String:  "",
					priv:    "",
					Int:     0,
					Slice:   []int{},
					Map:     map[int]int{},
					Bool:    false,
					Pointer: nil,
				},
			},
			want: demo{
				String: "mutated",
				priv:   "mutated",
				Int:    1,
				Slice:  []int{},
				Map:    map[int]int{},
				Bool:   true,
				Pointer: &demo{
					String:  "mutated",
					priv:    "mutated",
					Int:     1,
					Slice:   []int{},
					Map:     map[int]int{},
					Bool:    true,
					Pointer: nil,
				},
			},
		},
		{
			name: "mutate value",
			input: demo{
				String: "1",               // add mutated suffix
				priv:   "1",               // add mutated suffix
				Int:    1,                 // +1
				Slice:  []int{1},          // elements +1
				Map:    map[int]int{1: 2}, // according to different types
				Bool:   true,              // inverse
				Pointer: &demo{
					String:  "1",               // add mutated suffix
					priv:    "1",               // add mutated suffix
					Int:     1,                 // +1
					Slice:   []int{1},          // elements +1
					Map:     map[int]int{1: 2}, // according to different types
					Bool:    true,              // inverse
					Pointer: nil,
				},
			},
			want: demo{
				String: "1mutated",
				priv:   "1mutated",
				Int:    2,
				Slice:  []int{2},
				Map:    map[int]int{1: 1},
				Bool:   false,
				Pointer: &demo{
					String:  "1mutated",
					priv:    "1mutated",
					Int:     2,
					Slice:   []int{2},
					Map:     map[int]int{1: 1},
					Bool:    false,
					Pointer: nil,
				},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			MutateStruct(t, &c.input)
			if !reflect.DeepEqual(c.input, c.want) {
				t.Errorf("mutate result want %+v, but got: %+v", c.want, c.input)
			}
		})
	}
}

type (
	Bar struct{ f *Foo }
	Foo struct{ b *Bar }
)

func TestMutateCircularStruct(t *testing.T) {
	// foo -> bar -> foo...
	f := Foo{}
	b := Bar{}
	f.b = &b
	b.f = &f

	done := make(chan struct{})
	go func() {
		MutateStruct(t, &f)
		close(done)
	}()

	select {
	case <-time.After(time.Millisecond * 100):
		t.Error("test mutate circular data structure timeout")
	case <-done:
	}
}

func TestCorrectDeepCopy(t *testing.T) {
	cases := []struct {
		name         string
		deepCopyFunc func(d []string) []string
		input        []string
		correct      bool
	}{
		{
			name: "incorrect deepcopy",
			deepCopyFunc: func(d []string) []string {
				return d
			},
			input:   []string{"a"},
			correct: false,
		},
		{
			name: "correct deepcopy",
			// nolint: gocritic
			deepCopyFunc: func(d []string) []string {
				return slices.Clone(d)
			},
			input:   []string{"a"},
			correct: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fast := c.deepCopyFunc(c.input)
			slow := DeepCopySlow(c.input)

			// check copy is correct
			assert.Equal(t, c.input, fast)
			assert.Equal(t, c.input, slow)

			// check if it is a correct deepcopy
			// Assuming that `fast` is the result of deep copy, it is not affected by `input` mutation,
			// so it is equal to `slow` (`slow` is the result of deep copy).
			// If they are not equal, it proves that `fast` is not the result of `deep` copy.
			MutateStruct(t, &c.input)
			got := reflect.DeepEqual(fast, slow)
			if got != c.correct {
				t.Errorf("want %+v, but got %+v", c.correct, got)
			}
		})
	}
}
