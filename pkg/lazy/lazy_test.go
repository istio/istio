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

package lazy

import (
	"fmt"
	"sync"
	"testing"

	"go.uber.org/atomic"
	"golang.org/x/exp/slices"

	"istio.io/istio/pkg/test/util/assert"
	"istio.io/pkg/log"
)

func TestLazySerial(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		computations := atomic.NewInt32(0)
		l := NewWithRetry(func() (int32, error) {
			res := computations.Inc()
			if res > 2 {
				return res, nil
			}

			return res, fmt.Errorf("not yet")
		})
		res, err := l.Get()
		assert.Error(t, err)
		assert.Equal(t, res, 1)

		res, err = l.Get()
		assert.Error(t, err)
		assert.Equal(t, res, 2)

		res, err = l.Get()
		assert.NoError(t, err)
		assert.Equal(t, res, 3)
	})

	t.Run("no retry", func(t *testing.T) {
		computations := atomic.NewInt32(0)
		l := New(func() (int32, error) {
			res := computations.Inc()
			if res > 2 {
				return res, nil
			}

			return res, fmt.Errorf("not yet")
		})
		res, err := l.Get()
		assert.Error(t, err)
		assert.Equal(t, res, 1)

		res, err = l.Get()
		assert.Error(t, err)
		assert.Equal(t, res, 1)

		res, err = l.Get()
		assert.Error(t, err)
		assert.Equal(t, res, 1)
	})
}

func TestLazyRetry(t *testing.T) {
	computations := atomic.NewInt32(0)
	l := NewWithRetry(func() (int32, error) {
		res := computations.Inc()
		log.Infof("compute called, %v", res)
		if res > 5 {
			return res, nil
		}

		return res, fmt.Errorf("not yet")
	})
	wg := sync.WaitGroup{}
	wg.Add(10)
	results := []int32{}
	resCh := make(chan int32, 10)
	for i := 0; i < 10; i++ {
		go func() {
			res, err := l.Get()
			if res >= 6 {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
			resCh <- res
			wg.Done()
		}()
	}
	wg.Wait()
	close(resCh)
	for r := range resCh {
		results = append(results, r)
	}
	slices.Sort(results)
	assert.Equal(t, results, []int32{1, 2, 3, 4, 5, 6, 6, 6, 6, 6})
}

func TestLazy(t *testing.T) {
	computations := atomic.NewInt32(0)
	l := New(func() (int32, error) {
		res := computations.Inc()
		log.Infof("compute called, %v", res)
		if res > 5 {
			return res, nil
		}

		return res, fmt.Errorf("not yet")
	})
	wg := sync.WaitGroup{}
	wg.Add(10)
	results := []int32{}
	resCh := make(chan int32, 10)
	for i := 0; i < 10; i++ {
		go func() {
			res, err := l.Get()
			if res >= 6 {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
			resCh <- res
			wg.Done()
		}()
	}
	wg.Wait()
	close(resCh)
	for r := range resCh {
		results = append(results, r)
	}
	slices.Sort(results)
	assert.Equal(t, results, []int32{1, 1, 1, 1, 1, 1, 1, 1, 1, 1})
}
