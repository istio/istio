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
	"sync"
	"testing"
	"time"

	"istio.io/istio/pkg/envoy"
)

type EnvoyFactory struct {
	newEnvoyCh  chan *Envoy
	mutex       sync.Mutex
	creationErr error
}

func NewEnvoyFactory() *EnvoyFactory {
	return &EnvoyFactory{
		newEnvoyCh: make(chan *Envoy, 100),
	}
}

func (f *EnvoyFactory) New(cfg envoy.Config) (envoy.Instance, error) {
	creationErr := f.GetCreationError()
	if creationErr != nil {
		return nil, creationErr
	}

	e := &Envoy{
		config:      cfg,
		exitErrorCh: make(chan error, 1),
		waitCh:      make(chan struct{}, 1),
	}

	f.newEnvoyCh <- e

	return e, nil
}

func (f *EnvoyFactory) SetCreationError(err error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.creationErr = err
}

func (f *EnvoyFactory) GetCreationError() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return f.creationErr
}

func (f *EnvoyFactory) WaitForEnvoy(t *testing.T) *Envoy {
	t.Helper()
	return f.WaitForEnvoyWithDuration(t, time.Second*5)
}

func (f *EnvoyFactory) WaitForEnvoyWithDuration(t *testing.T, duration time.Duration) *Envoy {
	t.Helper()
	select {
	case e := <-f.newEnvoyCh:
		return e
	case <-time.After(duration):
		t.Fatal("envoy was never created")
		return nil
	}
}

func (f *EnvoyFactory) ExpectNoEnvoyCreated(t *testing.T) {
	t.Helper()
	f.ExpectNoEnvoyCreatedWithDuration(t, time.Second*1)
}

func (f *EnvoyFactory) ExpectNoEnvoyCreatedWithDuration(t *testing.T, duration time.Duration) {
	t.Helper()
	select {
	case <-f.newEnvoyCh:
		t.Fatal("envoy created unexpectedly")
	case <-time.After(duration):
		return
	}
}
