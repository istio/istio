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

package fakes

import (
	"github.com/lukechampine/freeze"

	"istio.io/istio/pilot/pkg/model"
)

type frozenIstioConfigStore struct {
	*IstioConfigStore
}

func (frozen *frozenIstioConfigStore) List(typ string, namespace string) ([]model.Config, error) {
	configs, err := frozen.IstioConfigStore.List(typ, namespace)
	if err != nil {
		return nil, err
	}
	return freeze.Slice(configs).([]model.Config), nil
}

func (fake *IstioConfigStore) Freeze() model.IstioConfigStore {
	return &frozenIstioConfigStore{IstioConfigStore: fake}
}
