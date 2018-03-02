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

package monitor_test

import (
	"errors"
	"testing"
	"time"

	"github.com/onsi/gomega"

	v3routing "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/config/monitor"
	"istio.io/istio/pilot/pkg/model"
)

const checkInterval = 100 * time.Millisecond

var createConfigSet = []*model.Config{
	{
		ConfigMeta: model.ConfigMeta{
			Name: "magic",
			Type: "gateway",
		},
		Spec: &v3routing.Gateway{
			Servers: []*v3routing.Server{
				{
					Port: &v3routing.Port{
						Number:   80,
						Protocol: "HTTP",
					},
					Hosts: []string{"*.example.com"},
				},
			},
		},
	},
}

var updateConfigSet = []*model.Config{
	{
		ConfigMeta: model.ConfigMeta{
			Name: "magic",
			Type: "gateway",
		},
		Spec: &v3routing.Gateway{
			Servers: []*v3routing.Server{
				{
					Port: &v3routing.Port{
						Number:   80,
						Protocol: "HTTPS",
					},
					Hosts: []string{"*.example.com"},
				},
			},
		},
	},
}

func TestMonitorForChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	configDescriptor := model.ConfigDescriptor{model.Gateway}

	store := memory.Make(configDescriptor)

	var (
		callCount int
		configs   []*model.Config
	)

	someConfigFunc := func() []*model.Config {
		switch callCount {
		case 0:
			configs = createConfigSet
		case 3:
			configs = updateConfigSet
		case 8:
			configs = []*model.Config{}
		}

		callCount++
		return configs
	}
	mon := monitor.NewMonitor(store, checkInterval, someConfigFunc)
	stop := make(chan struct{})
	defer func() { stop <- struct{}{} }() // shut it down
	mon.Start(stop)

	g.Eventually(func() error {
		c, err := store.List("gateway", "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		if len(c) != 1 {
			return errors.New("no configs")
		}

		if c[0].ConfigMeta.Name != "magic" {
			return errors.New("wrong config")
		}

		return nil
	}).Should(gomega.Succeed())

	g.Eventually(func() error {
		c, err := store.List("gateway", "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		gateway := c[0].Spec.(*v3routing.Gateway)
		if gateway.Servers[0].Port.Protocol != "HTTPS" {
			return errors.New("Protocol has not been updated")
		}

		return nil
	}).Should(gomega.Succeed())

	g.Eventually(func() ([]model.Config, error) {
		return store.List("gateway", "")
	}).Should(gomega.HaveLen(0))
}
