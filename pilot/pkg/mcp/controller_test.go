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
package coredatamodel_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/onsi/gomega"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	coredatamodel "istio.io/istio/pilot/pkg/mcp"
	"istio.io/istio/pilot/pkg/mcp/fakes"
	"istio.io/istio/pilot/pkg/model"
)

func TestRegisterEventHandler(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configDescriptor := model.ConfigDescriptor{}
	store := memory.Make(configDescriptor)
	logger := &fakes.Logger{}
	controller := coredatamodel.NewController(store, logger)

	var (
		registeredEvent     int
		registeredEventLock sync.Mutex
	)

	stop := make(chan struct{})
	go controller.Run(stop)
	defer func() {
		stop <- struct{}{}
	}()

	controller.RegisterEventHandler("virtual-service", func(model.Config, model.Event) {
		registeredEventLock.Lock()
		registeredEvent++
		registeredEventLock.Unlock()
	})

	g.Eventually(func() int {
		registeredEventLock.Lock()
		defer registeredEventLock.Unlock()
		return registeredEvent
	}).Should(gomega.Equal(1))
}

func TestConfigDescriptor(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configDescriptor := model.ConfigDescriptor{model.DestinationRule}
	store := memory.Make(configDescriptor)
	logger := &fakes.Logger{}
	controller := coredatamodel.NewController(store, logger)

	descriptors := controller.ConfigDescriptor()
	g.Expect(descriptors).To(gomega.Equal(model.IstioConfigTypes))
}

func TestGetInvalidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configDescriptor := model.ConfigDescriptor{}
	store := memory.Make(configDescriptor)
	logger := &fakes.Logger{}
	controller := coredatamodel.NewController(store, logger)

	c, exist := controller.Get("non-existent", "some-phony-name-space.com", "")
	g.Expect(c).To(gomega.BeNil())
	g.Expect(exist).To(gomega.BeFalse())

	g.Expect(logger.InfofCallCount()).To(gomega.Equal(1))
	format, message := logger.InfofArgsForCall(0)
	g.Expect(fmt.Sprintf(format, message...)).To(gomega.Equal("Get: type not supported: non-existent"))
}

func TestGetValidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configDescriptor := model.ConfigDescriptor{model.Gateway}
	store := memory.Make(configDescriptor)
	expectedConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name: "some-gateway",
			Type: "gateway",
		},
		Spec: &networking.Gateway{
			Servers: []*networking.Server{
				{
					Port: &networking.Port{
						Number:   80,
						Name:     "http",
						Protocol: "HTTP",
					},
					Hosts: []string{"*.example.com"},
				},
			},
		},
	}
	store.Create(expectedConfig)
	logger := &fakes.Logger{}
	controller := coredatamodel.NewController(store, logger)

	c, exist := controller.Get("gateway", "some-gateway", "")
	g.Expect(exist).To(gomega.BeTrue())
	g.Expect(c.Name).To(gomega.Equal(expectedConfig.Name))
	g.Expect(c.Type).To(gomega.Equal(expectedConfig.Type))
	g.Expect(c.Spec).To(gomega.Equal(expectedConfig.Spec))
}

func TestListInvalidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configDescriptor := model.ConfigDescriptor{model.Gateway}
	store := memory.Make(configDescriptor)
	logger := &fakes.Logger{}
	controller := coredatamodel.NewController(store, logger)

	c, err := controller.List("non-existent", "some-phony-name-space.com")
	g.Expect(c).To(gomega.BeNil())
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("type not supported"))
}

func TestListValidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configDescriptor := model.ConfigDescriptor{model.Gateway}
	store := memory.Make(configDescriptor)
	expectedConfig1 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name: "some-gateway",
			Type: "gateway",
		},
		Spec: &networking.Gateway{
			Servers: []*networking.Server{
				{
					Port: &networking.Port{
						Number:   80,
						Name:     "http",
						Protocol: "HTTP",
					},
					Hosts: []string{"*.example.com"},
				},
			},
		},
	}
	expectedConfig2 := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name: "some-gateway-2",
			Type: "gateway",
		},
		Spec: &networking.Gateway{
			Servers: []*networking.Server{
				{
					Port: &networking.Port{
						Number:   443,
						Name:     "https",
						Protocol: "HTTP",
					},
					Hosts: []string{"*.secure.example.com"},
				},
			},
		},
	}
	store.Create(expectedConfig1)
	store.Create(expectedConfig2)
	logger := &fakes.Logger{}
	controller := coredatamodel.NewController(store, logger)

	// Get gateways in all ("") namespaces
	c, err := controller.List("gateway", "")
	g.Expect(err).ToNot(gomega.HaveOccurred())
	for _, conf := range c {
		if conf.Name == expectedConfig1.Name {
			g.Expect(conf.Type).To(gomega.Equal(expectedConfig1.Type))
			g.Expect(conf.Spec).To(gomega.Equal(expectedConfig1.Spec))
		} else {
			g.Expect(conf.Name).To(gomega.Equal(expectedConfig2.Name))
			g.Expect(conf.Type).To(gomega.Equal(expectedConfig2.Type))
			g.Expect(conf.Spec).To(gomega.Equal(expectedConfig2.Spec))
		}
	}
}

func TestUpdateInvalidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configDescriptor := model.ConfigDescriptor{model.Gateway}
	store := memory.Make(configDescriptor)
	logger := &fakes.Logger{}
	controller := coredatamodel.NewController(store, logger)

	invalidConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name: "some-gateway-2",
			Type: "non-existent",
		},
		Spec: &networking.Gateway{
			Servers: []*networking.Server{
				{
					Port: &networking.Port{
						Number:   443,
						Name:     "https",
						Protocol: "HTTP",
					},
					Hosts: []string{"*.secure.example.com"},
				},
			},
		},
	}
	c, err := controller.Update(invalidConfig)
	g.Expect(c).To(gomega.BeEmpty())
	g.Expect(err).To(gomega.HaveOccurred())
	g.Expect(err.Error()).To(gomega.ContainSubstring("type not supported"))
}

func TestUpateValidType(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configDescriptor := model.ConfigDescriptor{model.Gateway}
	store := memory.Make(configDescriptor)
	existingConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:            "some-gateway",
			Type:            "gateway",
			ResourceVersion: "1",
		},
		Spec: &networking.Gateway{
			Servers: []*networking.Server{
				{
					Port: &networking.Port{
						Number:   80,
						Name:     "http",
						Protocol: "HTTP",
					},
					Hosts: []string{"*.example.com"},
				},
			},
		},
	}
	revision, _ := store.Create(existingConfig)
	logger := &fakes.Logger{}
	controller := coredatamodel.NewController(store, logger)

	updatedConfig := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name:            "some-gateway",
			Type:            "gateway",
			ResourceVersion: revision,
		},
		Spec: &networking.Gateway{
			Servers: []*networking.Server{
				{
					Port: &networking.Port{
						Number:   443,
						Name:     "https",
						Protocol: "HTTP",
					},
					Hosts: []string{"*.secure.example.com"},
				},
			},
		},
	}
	updatedRev, err := controller.Update(updatedConfig)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(updatedRev).ToNot(gomega.Equal(revision))

	c, exist := controller.Get("gateway", "some-gateway", "")
	g.Expect(exist).To(gomega.BeTrue())
	g.Expect(c.Type).To(gomega.Equal(updatedConfig.Type))
	g.Expect(c.Name).To(gomega.Equal(updatedConfig.Name))
	g.Expect(c.Spec).To(gomega.Equal(updatedConfig.Spec))
}
