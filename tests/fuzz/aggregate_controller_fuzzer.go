// Copyright Istio Authors
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

// nolint: revive
package fuzz

import (
	"fmt"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
)

var meshHolder fuzzMeshConfigHolder

type fuzzMeshConfigHolder struct {
	trustDomainAliases []string
}

func (mh fuzzMeshConfigHolder) Mesh() *meshconfig.MeshConfig {
	return &meshconfig.MeshConfig{
		TrustDomainAliases: mh.trustDomainAliases,
	}
}

// FuzzAggregateController implements a fuzzer
// that targets the add and delete registry apis
// of the aggregate controller. It does so by
// creating a controller with a pseudo-random
// Options{} and create pseudo-random service
// registries and deleting them.
func FuzzAggregateController(data []byte) int {
	ops := map[int]string{
		0: "AddRegistry",
		1: "DeleteRegistry",
	}
	maxOps := 2
	f := fuzz.NewConsumer(data)
	opts := aggregate.Options{}
	err := f.GenerateStruct(&opts)
	if err != nil {
		return 0
	}
	opts.MeshHolder = meshHolder
	c := aggregate.NewController(opts)

	iterations, err := f.GetInt()
	if err != nil {
		return 0
	}
	for i := 0; i < iterations%30; i++ {
		opType, err := f.GetInt()
		if err != nil {
			return 0
		}
		switch ops[opType%maxOps] {
		case "AddRegistry":
			err = runAddRegistry(f, c)
		case "DeleteRegistry":
			err = runDeleteRegistry(f, c)
		}
		if err != nil {
			return 0
		}
	}
	return 1
}

// Helper function to create a registry.
func runAddRegistry(f *fuzz.ConsumeFuzzer, c *aggregate.Controller) error {
	registry := serviceregistry.Simple{}
	err := f.GenerateStruct(&registry)
	if err != nil {
		return err
	}
	if registry.ServiceDiscovery == nil {
		return fmt.Errorf("registry required")
	}
	c.AddRegistry(registry)
	return nil
}

// Helper function to delete a registry.
func runDeleteRegistry(f *fuzz.ConsumeFuzzer, c *aggregate.Controller) error {
	registries := c.GetRegistries()
	if len(registries) == 0 {
		return fmt.Errorf("no registries")
	}
	index, err := f.GetInt()
	if err != nil {
		return err
	}
	selectedRegistry := registries[index%len(registries)]
	c.DeleteRegistry(selectedRegistry.Cluster(), selectedRegistry.Provider())
	return nil
}
