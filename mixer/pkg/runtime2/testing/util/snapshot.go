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

package util

import (
	"context"
	"fmt"

	"istio.io/istio/mixer/pkg/adapter"
	"istio.io/istio/mixer/pkg/config/storetest"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/template"
)

// GetSnapshot creates a config.Snapshot for testing purposes, based on the supplied configuration.
func GetSnapshot(templates map[string]*template.Info, adapters map[string]*adapter.Info, serviceConfig string, globalConfig string) *config.Snapshot {
	store, err := storetest.SetupStoreForTest(serviceConfig, globalConfig)
	if err != nil {
		panic(fmt.Sprintf("unable to crete store: %v", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := store.Init(ctx, config.KindMap(adapters, templates)); err != nil {
		panic(fmt.Sprintf("unable to initialize store: %v", err))
	}

	data := store.List()
	e := config.NewEphemeral(templates, adapters)
	e.SetState(data)

	cancel()

	return e.BuildSnapshot()
}
