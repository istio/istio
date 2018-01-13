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
	"io/ioutil"
	"os"
	"path"

	"istio.io/istio/mixer/pkg/adapter"
	cfg "istio.io/istio/mixer/pkg/config"
	"istio.io/istio/mixer/pkg/config/store"
	"istio.io/istio/mixer/pkg/runtime2/config"
	"istio.io/istio/mixer/pkg/template"
)

// GetSnapshot creates a config.Snapshot for testing purposes, based on the supplied configuration.
func GetSnapshot(templates map[string]*template.Info, adapters map[string]*adapter.Info, serviceConfig string, globalConfig string) *config.Snapshot {
	// TODO: This is a horrible hack, but it is the easiest way to get this up and running. We should avoid writing
	// files to the file-system and simply have an in-memory store for this.

	path, err := createConfigFiles(serviceConfig, globalConfig)
	if err != nil {
		panic(fmt.Sprintf("Unable to create config files: %s", err.Error()))
	}

	url := "fs://" + path

	reg2 := store.NewRegistry(cfg.StoreInventory()...)
	store2, err := reg2.NewStore(url)
	if err != nil {
		panic(fmt.Sprintf("unable to crete store2: %s", err.Error()))
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err = store2.Init(ctx, config.KindMap(adapters, templates)); err != nil {
		panic(fmt.Sprintf("unable to initialize store2: %s", err.Error()))
	}

	data := store2.List()
	e := config.NewEphemeral(templates, adapters)
	e.SetState(data)

	cancel()

	return e.BuildSnapshot()
}

func createConfigFiles(serviceConfig string, globalConfig string) (string, error) {
	dir, err := ioutil.TempDir("", "runtime2-testing")
	if err != nil {
		return "", err
	}
	s := path.Join(dir, "service.yaml")
	if err = ioutil.WriteFile(s, []byte(serviceConfig), 0666); err == nil {
		g := path.Join(dir, "global.yaml")
		if err = ioutil.WriteFile(g, []byte(globalConfig), 0666); err == nil {
			return dir, nil
		}

		_ = os.RemoveAll(dir)
	}

	return "", err
}
