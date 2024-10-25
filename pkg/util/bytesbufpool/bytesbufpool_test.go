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

package bytesbufpool

import (
	"bytes"
	"sync"
	"testing"
)

func TestBytesBufferPool(t *testing.T) {
	wg := sync.WaitGroup{}
	pool := New()
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b := pool.Get()
			if b.Len() != 0 {
				t.Errorf("want len 0, but got %d", b.Len())
			}
			if b.Cap() != 64 {
				t.Errorf("want cap 64(the default cap), but got %d", b.Cap())
			}
			b.WriteString("hello")
			pool.Put(b)
		}()
	}
	wg.Wait()
}

func TestGetWithCap(t *testing.T) {
	pool := NewWithCap(10)
	b := pool.Get()
	if b.Len() != 0 {
		t.Errorf("want len 0, but got %d", b.Len())
	}
	if b.Cap() != 10 {
		t.Errorf("want cap is 10 , but got %d", b.Cap())
	}
	pool.Put(b)
}

func BenchmarkCompare(b *testing.B) {
	b.Run("pool with small string", func(b *testing.B) {
		pool := New()
		for i := 0; i < b.N; i++ {
			buf := pool.Get()
			buf.WriteString("small string")
			_ = buf.String()
			pool.Put(buf)
		}
	})

	b.Run("pool with long string", func(b *testing.B) {
		pool := New()
		for i := 0; i < b.N; i++ {
			buf := pool.Get()
			buf.WriteString("Istio extends Kubernetes to establish a programmable, application-aware network.")
			buf.WriteString("Istio extends Kubernetes to establish a programmable, application-aware network.")
			buf.WriteString("Istio extends Kubernetes to establish a programmable, application-aware network.")
			_ = buf.String()
			pool.Put(buf)
		}
	})

	b.Run("bytes buffer with small string", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := bytes.Buffer{}
			buf.WriteString("small string")
			_ = buf.String()
		}
	})

	b.Run("bytes buffer with long string", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			buf := bytes.Buffer{}
			buf.WriteString("Istio extends Kubernetes to establish a programmable, application-aware network.")
			buf.WriteString("Istio extends Kubernetes to establish a programmable, application-aware network.")
			buf.WriteString("Istio extends Kubernetes to establish a programmable, application-aware network.")
			_ = buf.String()
		}
	})
}
