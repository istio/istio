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

package dispatcher

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

//
//func BenchmarkChannelMemUsage(b *testing.B) {
//	for i := 0; i < b.N; i++ {
//		_ = make(chan *result, 1)
//		//for j := 0; j < 1; j++ {
//		//	c <- nil
//		//}
//		//close(c)
//	}
//}

type selector struct {
	targets []*target
	current int32
}

type array struct {
	targets [2]*target
	current int32
}

type fixed struct {
	target *target
}

type locked struct {
	target *target
	lock   sync.RWMutex
}

type target struct {
	value int
}

func (s *fixed) dispatch(v int) {
	s.target.work(v)
}

func (s *selector) dispatch(v int) {
	s.targets[atomic.LoadInt32(&s.current)%2].work(v)
}

func (s *array) dispatch(v int) {
	s.targets[atomic.LoadInt32(&s.current)%2].work(v)
}

func (s *locked) dispatch(v int) {
	s.lock.RLock()
	t := s.target
	s.lock.RUnlock()
	t.work(v)
}

func (s *fixed) update(t *target) {
	// Do nothing
}

func (s *selector) update(t *target) {
	slot := (atomic.LoadInt32(&s.current) + 1) % 2
	s.targets[slot] = t
	atomic.AddInt32(&s.current, 1)
}

func (s *array) update(t *target) {
	slot := (atomic.LoadInt32(&s.current) + 1) % 2
	s.targets[slot] = t
	atomic.AddInt32(&s.current, 1)
}

func (s *locked) update(t *target) {
	s.lock.Lock()
	s.target = t
	s.lock.Unlock()
}

func (t *target) work(v int) {
	t.value += v
	t.value %= 1024
	t.value *= 2
}

func BenchmarkSelector(b *testing.B) {
	s := &selector{}
	s.targets = []*target{&target{}, &target{}}

	done := false
	go func() {
		for !done {
			time.Sleep(time.Millisecond)
			s.update(&target{})
		}
	}()

	b.Run("rs", func(bb *testing.B) {
		for i := 0; i < bb.N; i++ {
			s.dispatch(i)
		}
	})

	done = true
}

func BenchmarkArray(b *testing.B) {
	s := &array{}
	s.targets[0] = &target{}
	s.targets[1] = &target{}

	done := false
	go func() {
		for !done {
			time.Sleep(time.Millisecond)
			s.update(&target{})
		}
	}()

	b.Run("rs", func(bb *testing.B) {
		for i := 0; i < bb.N; i++ {
			s.dispatch(i)
		}
	})

	done = true
}

func BenchmarkFixed(b *testing.B) {
	s := &fixed{}
	s.target = &target{}

	done := false
	go func() {
		for !done {
			time.Sleep(time.Millisecond)
			s.update(&target{})
		}
	}()

	b.Run("rs", func(bb *testing.B) {
		for i := 0; i < bb.N; i++ {
			s.dispatch(i)
		}
	})

	done = true
}

func BenchmarkLocked(b *testing.B) {
	s := &locked{}
	s.target = &target{}

	done := false
	go func() {
		for !done {
			time.Sleep(time.Millisecond)
			s.update(&target{})
		}
	}()

	b.Run("rs", func(bb *testing.B) {
		for i := 0; i < bb.N; i++ {
			s.dispatch(i)
		}
	})

	done = true
}

func BenchmarkWaitGroup(b *testing.B) {
	var g sync.WaitGroup

	b.Run("bb", func(bb *testing.B) {
		for i := 0; i < bb.N; i++ {
			g.Add(1)
			g.Done()
		}
	})
}

func BenchmarkRefCount(b *testing.B) {
	var r int32

	for i := 0; i < b.N; i++ {
		atomic.AddInt32(&r, 1)
		atomic.AddInt32(&r, -1)
	}
}
