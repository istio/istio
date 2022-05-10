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

package forwarder

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	"golang.org/x/sync/semaphore"
)

const (
	maxConcurrencyPerForward = 20
)

type executor struct {
	totalRequests  *atomic.Uint64
	activeRequests *atomic.Uint64
	stopCh         chan struct{}
}

func newExecutor() *executor {
	e := &executor{
		totalRequests:  atomic.NewUint64(0),
		activeRequests: atomic.NewUint64(0),
		stopCh:         make(chan struct{}),
	}

	return e
}

func (e *executor) ActiveRequests() uint64 {
	return e.activeRequests.Load()
}

// NewGroup creates a new group of tasks that can be managed collectively.
// Parallelism is limited by the global maxConcurrency of the executor.
func (e *executor) NewGroup() *execGroup {
	return &execGroup{
		e:   e,
		sem: semaphore.NewWeighted(int64(maxConcurrencyPerForward)),
	}
}

type execGroup struct {
	e   *executor
	g   multierror.Group
	sem *semaphore.Weighted
}

// Go runs the given work function asynchronously.
func (g *execGroup) Go(ctx context.Context, work func() error) {
	g.g.Go(func() error {
		g.e.totalRequests.Inc()
		g.e.activeRequests.Inc()
		defer g.e.activeRequests.Dec()

		// Acquire the group concurrency semaphore.
		if err := g.sem.Acquire(ctx, 1); err != nil {
			return fmt.Errorf("request set timed out: %v", err)
		}
		defer g.sem.Release(1)

		return work()
	})
}

func (g *execGroup) Wait() *multierror.Error {
	return g.g.Wait()
}

func (e *executor) Close() {
	close(e.stopCh)
}
