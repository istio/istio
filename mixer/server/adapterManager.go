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

package main

import (
	"errors"

	"github.com/istio/mixer/adapters"

	"github.com/istio/mixer/adapters/denyChecker"
	"github.com/istio/mixer/adapters/factMapper"
	"github.com/istio/mixer/adapters/ipListChecker"
)

// all the known fact converter adapter types
var factConverters = []adapters.Adapter{
	factMapper.NewAdapter(),
}

// all the known list checker adapter types
var listCheckers = []adapters.Adapter{
	ipListChecker.NewAdapter(),
	denyChecker.NewAdapter(),
}

// AdapterManager keeps track of activated Adapter objects for different types of adapters
type AdapterManager struct {
	// FactConverters is the set of fact converter adapters
	FactConverters map[string]adapters.Adapter

	// ListCheckers is the set of list checker adapters
	ListCheckers map[string]adapters.Adapter
}

// TODO: this implementation needs to be driven from external config instead of being
// hardcoded
func prepAdapters(l []adapters.Adapter) (map[string]adapters.Adapter, error) {
	m := make(map[string]adapters.Adapter, len(l))

	for _, adapter := range l {
		if _, exists := m[adapter.Name()]; exists {
			return nil, errors.New("can't have multiple adapters called " + adapter.Name())
		}

		m[adapter.Name()] = adapter
	}

	return m, nil
}

// NewAdapterManager returns a new AdapterManager instance
func NewAdapterManager() (*AdapterManager, error) {
	var mgr = AdapterManager{}
	var err error

	if mgr.FactConverters, err = prepAdapters(factConverters); err != nil {
		return nil, err
	}

	if mgr.ListCheckers, err = prepAdapters(listCheckers); err != nil {
		return nil, err
	}

	return &mgr, nil
}
