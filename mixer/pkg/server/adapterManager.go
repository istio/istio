// Copyright 2016 Google Inc.
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

package server

import (
	"errors"

	"fmt"

	"istio.io/mixer/adapter/jsonLogger"
	"istio.io/mixer/pkg/adapter"
)

// all the known list checker adapter implementations
var listCheckers = []adapter.Adapter{}

var loggers = []adapter.Adapter{
	jsonLogger.NewAdapter(),
}

// AdapterManager keeps track of activated Aspect objects for different types of adapters
type AdapterManager struct {
	// ListCheckers is the set of list checker adapters
	ListCheckers map[string]adapter.Adapter

	// Loggers is the set of logger adapters
	Loggers map[string]adapter.Adapter
}

// GetListCheckerAdapter returns a matching adapter for the given dispatchKey. If there is no existing adapter,
// it instantiates one, based on the provided adapter config.
func (mgr *AdapterManager) GetListCheckerAdapter(dispatchKey DispatchKey, config *adapter.AspectConfig) (adapter.ListChecker, error) {
	// TODO: instantiation & caching of the adapter.
	return nil, errors.New("NYI")
}

// GetLoggerAdapter returns a matching adapter for the given dispatchKey. If there is no existing adapter,
// it instantiates one, based on the provided adapter config.
func (mgr *AdapterManager) GetLoggerAdapter(dispatchKey DispatchKey, config *adapter.AspectConfig) (adapter.Logger, error) {
	a := mgr.Loggers[(*config).Name()]
	// TODO: NewAspect probably should take a pointer, instead of value.
	aspectState, err := a.NewAspect(*config)
	if err != nil {
		return nil, err
	}

	loggerAdapter, ok := aspectState.(adapter.Logger)
	if !ok {
		return nil, fmt.Errorf("the aspect does not implement the Logger interface: adapter='%v'", a.Name())
	}

	return loggerAdapter, nil
}

// TODO: this implementation needs to be driven from external config instead of being hardcoded
func prepBuilders(l []adapter.Adapter) (map[string]adapter.Adapter, error) {
	m := make(map[string]adapter.Adapter, len(l))

	for _, adapter := range l {
		if _, exists := m[adapter.Name()]; exists {
			return nil, errors.New("can't have multiple builders called " + adapter.Name())
		}

		if err := adapter.Configure(adapter.DefaultAdapterConfig()); err != nil {
			return nil, err
		}

		m[adapter.Name()] = adapter
	}

	return m, nil
}

// NewAdapterManager returns a new AdapterManager instance
func NewAdapterManager() (*AdapterManager, error) {
	var mgr = AdapterManager{}
	var err error

	if mgr.ListCheckers, err = prepBuilders(listCheckers); err != nil {
		return nil, err
	}

	if mgr.Loggers, err = prepBuilders(loggers); err != nil {
		return nil, err
	}

	return &mgr, nil
}
