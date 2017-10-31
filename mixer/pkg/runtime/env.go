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

package runtime

import (
	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/pool"
)

// TODO this file is copied from pkg/adapterManager.
// Remove the adapterManager package once adapters2 switch is complete.

type env struct {
	logger adapter.Logger
	gp     *pool.GoroutinePool
}

func newEnv(name string, gp *pool.GoroutinePool) adapter.Env {
	return env{
		logger: newLogger(name),
		gp:     gp,
	}
}

func (e env) Logger() adapter.Logger {
	return e.logger
}

func (e env) ScheduleWork(fn adapter.WorkFunc) {
	e.gp.ScheduleWork(func() {
		defer func() {
			if r := recover(); r != nil {
				_ = e.Logger().Errorf("Adapter worker failed: %v", r) // nolint: gas

				// TODO: Beyond logging, we want to do something proactive here.
				//       For example, we want to probably terminate the originating
				//       adapter and record the failure so we can count how often
				//       it happens, etc.
			}
		}()

		fn()
	})
}

func (e env) ScheduleDaemon(fn adapter.DaemonFunc) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				_ = e.Logger().Errorf("Adapter daemon failed: %v", r) // nolint: gas

				// TODO: Beyond logging, we want to do something proactive here.
				//       For example, we want to probably terminate the originating
				//       adapter and record the failure so we can count how often
				//       it happens, etc.
			}
		}()

		fn()
	}()
}
