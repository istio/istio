// Copyright 2017 Istio Authors.
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

package appoptics

import (
	"istio.io/istio/mixer/adapter/appoptics/papertrail"
	"istio.io/istio/mixer/pkg/adapter"
)

type adapterEnvInst struct {
}

func (a *adapterEnvInst) Logger() adapter.Logger {
	return &papertrail.LoggerImpl{}
}

func (a *adapterEnvInst) ScheduleWork(fn adapter.WorkFunc) {
}

func (a *adapterEnvInst) ScheduleDaemon(fn adapter.DaemonFunc) {
}
