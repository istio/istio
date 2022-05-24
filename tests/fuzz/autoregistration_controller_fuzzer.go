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

package fuzz

import (
	"fmt"
	"time"

	fuzz "github.com/AdaLogics/go-fuzz-headers"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/autoregistration"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/keepalive"
)

var (
	// A valid WorkloadGroup.
	// This can be modified to have pseudo-random
	// values for more randomization.
	tmplA = &v1alpha3.WorkloadGroup{
		Template: &v1alpha3.WorkloadEntry{
			Ports:          map[string]uint32{"http": 80},
			Labels:         map[string]string{"app": "a"},
			Network:        "nw0",
			Locality:       "reg0/zone0/subzone0",
			Weight:         1,
			ServiceAccount: "sa-a",
		},
	}
	// A valid Config.
	// This can be modified to have pseudo-random
	// values for more randomization.
	wgA = config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WorkloadGroup,
			Namespace:        "a",
			Name:             "wg-a",
			Labels: map[string]string{
				"grouplabel": "notonentry",
			},
		},
		Spec:   tmplA,
		Status: nil,
	}
)

// FuzzWE implements a fuzzer that targets several apis
// in the workloadentry package. It does so by setting
// up a workloadentry controller with a proxy with
// pseudo-random values.
// The fuzzer then uses the controller to test:
// 1: RegisterWorkload
// 2: QueueUnregisterWorkload
// 3: QueueWorkloadEntryHealth
func FuzzWE(data []byte) int {
	f := fuzz.NewConsumer(data)
	proxy := &model.Proxy{}
	err := f.GenerateStruct(proxy)
	if err != nil {
		return 0
	}
	if !ProxyValid(proxy) {
		return 0
	}

	store := memory.NewController(memory.Make(collections.All))
	c := autoregistration.NewController(store, "", keepalive.Infinity)
	err = createStore(store, wgA)
	if err != nil {
		fmt.Println(err)
		return 0
	}
	stop := make(chan struct{})
	go c.Run(stop)
	defer close(stop)

	err = c.RegisterWorkload(proxy, time.Now())
	if err != nil {
		return 0
	}
	c.QueueUnregisterWorkload(proxy, time.Now())

	he := autoregistration.HealthEvent{}
	err = f.GenerateStruct(&he)
	if err != nil {
		return 0
	}
	c.QueueWorkloadEntryHealth(proxy, he)

	return 1
}

// Helper function to create a store.
func createStore(store model.ConfigStoreController, cfg config.Config) error {
	if _, err := store.Create(cfg); err != nil {
		return err
	}
	return nil
}
