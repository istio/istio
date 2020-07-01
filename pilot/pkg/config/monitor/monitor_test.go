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

package monitor

import (
	"errors"
	"testing"
	"time"

	"github.com/onsi/gomega"

	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
)

var createConfigSet = []*model.Config{
	{
		ConfigMeta: model.ConfigMeta{
			Name:             "magic",
			GroupVersionKind: gvk.Gateway,
		},
		Spec: &networking.Gateway{
			Servers: []*networking.Server{
				{
					Port: &networking.Port{
						Number:   80,
						Protocol: "HTTP",
						Name:     "http",
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
			Name:             "magic",
			GroupVersionKind: gvk.Gateway,
		},
		Spec: &networking.Gateway{
			Servers: []*networking.Server{
				{
					Port: &networking.Port{
						Number:   80,
						Protocol: "HTTP2",
						Name:     "http",
					},
					Hosts: []string{"*.example.com"},
				},
			},
		},
	},
}

func TestMonitorForChange(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store := memory.Make(collection.SchemasFor(collections.IstioNetworkingV1Alpha3Gateways))

	var (
		callCount int
		configs   []*model.Config
		err       error
	)

	someConfigFunc := func() ([]*model.Config, error) {
		switch callCount {
		case 0:
			configs = createConfigSet
			err = nil
		case 3:
			configs = updateConfigSet
		case 6:
			configs = []*model.Config{}
		}

		callCount++
		return configs, err
	}
	mon := NewMonitor("", store, someConfigFunc, "")
	stop := make(chan struct{})
	defer func() { stop <- struct{}{} }() // shut it down
	mon.Start(stop)

	go func() {
		for i := 0; i < 10; i++ {
			mon.updateCh <- struct{}{}
			time.Sleep(time.Millisecond * 100)
		}
	}()
	g.Eventually(func() error {
		c, err := store.List(gvk.Gateway, "")
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
		c, err := store.List(gvk.Gateway, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())
		if len(c) == 0 {
			return errors.New("no config")
		}

		gateway := c[0].Spec.(*networking.Gateway)
		if gateway.Servers[0].Port.Protocol != "HTTP2" {
			return errors.New("protocol has not been updated")
		}

		return nil
	}).Should(gomega.Succeed())

	g.Eventually(func() ([]model.Config, error) {
		return store.List(gvk.Gateway, "")
	}).Should(gomega.HaveLen(0))

}

func TestMonitorForError(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store := memory.Make(collection.SchemasFor(collections.IstioNetworkingV1Alpha3Gateways))

	var (
		callCount int
		configs   []*model.Config
		err       error
	)

	delay := make(chan struct{}, 1)

	someConfigFunc := func() ([]*model.Config, error) {
		switch callCount {
		case 0:
			configs = createConfigSet
			err = nil
		case 3:
			configs = nil
			err = errors.New("snapshotFunc can't connect")
			delay <- struct{}{}
		}

		callCount++
		return configs, err
	}
	mon := NewMonitor("", store, someConfigFunc, "")
	stop := make(chan struct{})
	defer func() { stop <- struct{}{} }() // shut it down
	mon.Start(stop)

	go func() {
		for i := 0; i < 10; i++ {
			mon.updateCh <- struct{}{}
			time.Sleep(time.Millisecond * 10)
		}
	}()
	//Test ensures that after a coplilot connection error the data remains
	//nil data return and error return keeps the existing data aka createConfigSet
	<-delay
	g.Eventually(func() error {
		c, err := store.List(gvk.Gateway, "")
		g.Expect(err).NotTo(gomega.HaveOccurred())

		if len(c) != 1 {
			return errors.New("config files erased on Copilot error")
		}

		return nil
	}).Should(gomega.Succeed())
}
