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

package handler

import (
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/pool"
)

type env struct {
	logger   adapter.Logger
	gp       *pool.GoroutinePool
	counters envCounters
}

// NewEnv returns a new environment instance.
func NewEnv(cfgID int64, name string, gp *pool.GoroutinePool) adapter.Env {
	return env{
		logger:   newLogger(name),
		gp:       gp,
		counters: newEnvCounters(cfgID, name),
	}
}

// Logger from adapter.Env.
func (e env) Logger() adapter.Logger {
	return e.logger
}

// ScheduleWork from adapter.Env.
func (e env) ScheduleWork(fn adapter.WorkFunc) {
	e.counters.workers.Inc()

	// TODO (Issue #2503): This method creates a closure which causes allocations. We can ensure that we're
	// not creating a closure by calling a method by name, instead of using an anonymous one.
	e.gp.ScheduleWork(func(ifn interface{}) {
		reachedEnd := false

		defer func() {
			// Always decrement the worker count.
			e.counters.workers.Dec()

			if !reachedEnd {
				r := recover()
				_ = e.Logger().Errorf("Adapter worker failed: %v", r)

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
	e.counters.daemons.Inc()

	go func() {
		reachedEnd := false

		defer func() {
			// Always decrement the daemon count.
			e.counters.daemons.Dec()

			if !reachedEnd {
				r := recover()
				_ = e.Logger().Errorf("Adapter daemon failed: %v", r)

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

func (e env) reportStrayWorkers() error {
	if dc, err := e.counters.getDaemonCount(); err != nil {
		return err
	} else if dc > 0 {
		// TODO: ideally we should return some sort of error here to bubble up this issue to the top so that
		// operator can look at it. However, currently we cannot guarantee that SchedularXXXX gauge
		// counter will give consistent value because of timing issue in the ScheduleWorker and ScheduleDaemon.
		// Basically, even if the adapter would have closed everything before returning from Close function, our
		// counter might get delayed decremented, causing this false positive error.
		// Therefore, we need a new retry kind logic on handler Close to give time for counters to get updated
		// before making this as a red flag error. runtime work has plans to implement this stuff, we can revisit
		// this to-do then. Same for the code below related to workers.
		_ = e.Logger().Errorf("adapter did not close all the scheduled daemons")
	}

	if wc, err := e.counters.getWorkerCount(); err != nil {
		return err
	} else if wc > 0 {
		_ = e.Logger().Errorf("adapter did not close all the scheduled workers")
	}

	return nil
}
