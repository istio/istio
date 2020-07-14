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

package handler

import (
	"context"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/runtime/monitoring"
	"istio.io/istio/pkg/listwatch"
	"istio.io/pkg/log"
	"istio.io/pkg/pool"
)

type env struct {
	logger           adapter.Logger
	gp               *pool.GoroutinePool
	monitoringCtx    context.Context
	daemons, workers *int64
	namespaces       *[]string
}

// NewEnv returns a new environment instance.
func NewEnv(cfgID int64, name string, gp *pool.GoroutinePool, namespaces []string) adapter.Env {
	ctx := context.Background()
	var err error
	if ctx, err = tag.New(ctx, tag.Insert(monitoring.HandlerTag, name)); err != nil {
		log.Errorf("could not setup context for stats: %v", err)
	}
	return env{
		logger:        newLogger(name),
		gp:            gp,
		monitoringCtx: ctx,
		daemons:       new(int64),
		workers:       new(int64),
		namespaces:    &namespaces,
	}
}

// Logger from adapter.Env.
func (e env) Logger() adapter.Logger {
	return e.logger
}

// ScheduleWork from adapter.Env.
func (e env) ScheduleWork(fn adapter.WorkFunc) {
	stats.Record(e.monitoringCtx, monitoring.WorkersTotal.M(atomic.AddInt64(e.workers, 1)))

	// TODO (Issue #2503): This method creates a closure which causes allocations. We can ensure that we're
	// not creating a closure by calling a method by name, instead of using an anonymous one.
	e.gp.ScheduleWork(func(ifn interface{}) {
		reachedEnd := false

		defer func() {
			// Always decrement the worker count.
			stats.Record(e.monitoringCtx, monitoring.WorkersTotal.M(atomic.AddInt64(e.workers, -1)))

			if !reachedEnd {
				r := recover()
				_ = e.Logger().Errorf("Adapter worker failed: %v", r) // nolint: gas

				// TODO (Issue #2503): Beyond logging, we want to do something proactive here.
				//       For example, we want to probably terminate the originating
				//       adapter and record the failure so we can count how often
				//       it happens, etc.
			}
		}()

		ifn.(adapter.WorkFunc)()
		reachedEnd = true
	}, fn)
}

// ScheduleDaemon from adapter.Env.
func (e env) ScheduleDaemon(fn adapter.DaemonFunc) {
	stats.Record(e.monitoringCtx, monitoring.DaemonsTotal.M(atomic.AddInt64(e.daemons, 1)))

	go func() {
		reachedEnd := false

		defer func() {
			// Always decrement the daemon count.
			e.Logger().Infof("shutting down daemon...")
			stats.Record(e.monitoringCtx, monitoring.DaemonsTotal.M(atomic.AddInt64(e.daemons, -1)))

			if !reachedEnd {
				r := recover()
				_ = e.Logger().Errorf("Adapter daemon failed: %v", r) // nolint: gas

				// TODO (Issue #2503): Beyond logging, we want to do something proactive here.
				//       For example, we want to probably terminate the originating
				//       adapter and record the failure so we can count how often
				//       it happens, etc.
			}
		}()

		fn()
		reachedEnd = true
	}()
}

func (e env) Workers() int64 {
	return atomic.LoadInt64(e.workers)
}

func (e env) Daemons() int64 {
	return atomic.LoadInt64(e.daemons)
}

func (e env) hasStrayWorkers() bool {
	return e.Daemons() > 0 || e.Workers() > 0
}

func (e env) reportStrayWorkers() {
	if atomic.LoadInt64(e.daemons) > 0 {
		_ = e.Logger().Errorf("adapter did not close all the scheduled daemons")
	}

	if atomic.LoadInt64(e.workers) > 0 {
		_ = e.Logger().Errorf("adapter did not close all the scheduled workers")
	}
}

func (e env) NewInformer(
	clientset kubernetes.Interface,
	objType runtime.Object,
	duration time.Duration,
	listerWatcher func(namespace string) cache.ListerWatcher,
	indexers cache.Indexers) cache.SharedIndexInformer {

	mlw := listwatch.MultiNamespaceListerWatcher(*e.namespaces, listerWatcher)
	return cache.NewSharedIndexInformer(mlw, objType, duration, indexers)
}
