// Copyright 2019 Istio Authors
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

package attribute

import "testing"

const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func getLargeBag() *MutableBag {
	m := map[string]interface{}{}

	for _, l := range alphabet {
		m[string(l)] = string(l) + "-val"
	}

	return GetMutableBagForTesting(m)
}

// Original        100000	     20527 ns/op	    9624 B/op	      12 allocs/op
// with Contains() 1000000	      1997 ns/op	       0 B/op	       0 allocs/op
func Benchmark_MutableBagMerge(b *testing.B) {

	mergeTo := mutableBagFromProtoForTesing()
	mergeFrom := getLargeBag()

	// bag.merge will only update an element if one is not present.
	// bag.Merge(bag) should be a no

	b.Run("LargeMerge", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			mergeTo.Merge(mergeFrom)
		}
	})

}
