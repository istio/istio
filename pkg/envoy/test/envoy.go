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

package test

import (
	"context"
	"sync"
	"time"

	envoyAdmin "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"

	"istio.io/istio/pkg/envoy"
)

var _ envoy.Instance = &Envoy{}

type Envoy struct {
	config            envoy.Config
	exitErrorCh       chan error
	waitCh            chan struct{}
	waitErr           error
	mutex             sync.Mutex
	customDoneHandler func()
}

func (e *Envoy) Start(ctx context.Context) envoy.Instance {
	go func() {
		defer close(e.waitCh)

		select {
		case err := <-e.exitErrorCh:
			e.waitErr = err
		case <-ctx.Done():
			e.waitErr = ctx.Err()

			h := e.GetCustomDoneHandler()
			if h != nil {
				h()
			}
		}
	}()
	return e
}

func (e *Envoy) Config() envoy.Config {
	return e.config
}

func (e *Envoy) ConfigPath() string {
	return e.getOption(envoy.ConfigPath("").FlagName()).FlagValue()
}

func (e *Envoy) ConfigYaml() string {
	return e.getOption(envoy.ConfigYaml("").FlagName()).FlagValue()
}

func (e *Envoy) BaseID() envoy.BaseID {
	return e.getOption(envoy.InvalidBaseID.FlagName()).(envoy.BaseID)
}

func (e *Envoy) Epoch() envoy.Epoch {
	return e.getOption(envoy.Epoch(0).FlagName()).(envoy.Epoch)
}

func (e *Envoy) getOption(flagName envoy.FlagName) envoy.Option {
	for _, o := range e.Config().Options {
		if o.FlagName() == flagName {
			return o
		}
	}
	panic("unable to locate option for " + flagName)
}

func (e *Envoy) SetWaitErr(err error) {
	e.waitErr = err
}

func (e *Envoy) Wait() envoy.Waitable {
	return &waitable{Envoy: e}
}

type waitable struct {
	*Envoy
	timeout time.Duration
}

func (w *waitable) WithTimeout(timeout time.Duration) envoy.Waitable {
	return &waitable{
		Envoy:   w.Envoy,
		timeout: timeout,
	}
}

func (w *waitable) Do() error {
	var timeoutCh <-chan time.Time
	if w.timeout > 0 {
		timeoutCh = time.After(w.timeout)
	} else {
		timeoutCh = make(chan time.Time, 1)
	}

	select {
	case <-w.waitCh:
		break
	case <-timeoutCh:
		return context.DeadlineExceeded
	}
	return w.waitErr
}

func (e *Envoy) SetCustomDoneHandler(handler func()) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.customDoneHandler = handler
}

func (e *Envoy) GetCustomDoneHandler() func() {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	return e.customDoneHandler
}

func (e *Envoy) Exit(err error) {
	e.exitErrorCh <- err
}

func (e *Envoy) WaitLive() envoy.Waitable {
	panic("not implemented")
}

func (e *Envoy) AdminPort() uint32 {
	panic("not implemented")
}

func (e *Envoy) GetServerInfo() (*envoyAdmin.ServerInfo, error) {
	panic("not implemented")
}

func (e *Envoy) GetConfigDump() (*envoyAdmin.ConfigDump, error) {
	panic("not implemented")
}

func (e *Envoy) WaitFor(duration time.Duration) error {
	panic("not implemented")
}

func (e *Envoy) Kill() error {
	panic("not implemented")
}

func (e *Envoy) KillAndWait() envoy.Waitable {
	panic("not implemented")
}

func (e *Envoy) Shutdown() error {
	panic("not implemented")
}

func (e *Envoy) ShutdownAndWait() envoy.Waitable {
	panic("not implemented")
}
