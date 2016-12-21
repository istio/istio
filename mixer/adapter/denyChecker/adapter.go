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

package denyChecker

import (
	"istio.io/mixer/pkg/adapter"
)

// AdapterConfig is used to configure adapters.
type AdapterConfig struct {
	adapter.AdapterConfig
}

type adapterState struct {
}

// newAdapter returns a new adapter.
func newAdapter(config *AdapterConfig) (adapter.ListChecker, error) {
	return &adapterState{}, nil
}

func (a *adapterState) Close() error {
	return nil
}

func (a *adapterState) CheckList(symbol string) (bool, error) {
	return false, nil
}
