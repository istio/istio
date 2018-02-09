// Copyright 2018 Istio Authors
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

package baseline

import (
	"fmt"
	"math/rand"
	"testing"
)

var seed int64 = 1518151537946196000
var size = 1024

func BenchmarkBaseline(b *testing.B) {
	r := rand.New(rand.NewSource(seed))
	head := allocList(size, r)
	m := allocMap(size, r)
	b.Run("B", func(bb *testing.B) {
		for i := 0; i < bb.N; i++ {
			randomWire(1024, head, r)
			randomJump(1024, m, r)
		}
	})
}

type node struct {
	ddata int
	sdata string
	next  *node
	prev  *node
	other *node
}

func randomJump(count int, m map[int]node, r *rand.Rand) {
	n := r.Intn(len(m))
	for i := 0; i < count; i++ {
		s := m[n].sdata

		n = m[n].ddata

		node := m[n]
		node.sdata = s
		m[n] = node

		if n % 13 == 0 {
			n = i % len(m)
		}
	}
}

func allocMap(len int, r *rand.Rand) map[int]node {
	m := make(map[int]node, len)

	for i := 0; i < len; i++ {
		m[i] = node {
			ddata: r.Intn(len),
			sdata: fmt.Sprintf("%v", r.Float64()),
		}
	}

	return m
}

func allocList(len int, r *rand.Rand) *node {
	head := &node{
		ddata: 0,
		sdata: "",
	}
	head.next = head
	head.prev = head

	p := head
	for i := 0; i < len; i++ {
		n := &node{
			ddata: r.Int(),
			sdata: fmt.Sprintf("%v", r.Float64()),
		}

		p.next = n
		n.prev = p
		n.next = head
		head.prev = n
		p = n
	}

	return head
}

func randomWire(count int, head *node, r *rand.Rand) {
	cur := head
	for i := 0; i < count; i++ {
		c := cur
		l := r.Intn(1024)
		for j := 0; j < l; j++ {
			if l%2 == 0 {
				cur = cur.next
			} else {
				cur = cur.prev
			}
			t := cur.other
			cur.other = c
			if t != nil {
				cur = t
			}
		}
	}
}
