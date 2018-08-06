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
	"testing"

	"github.com/gogo/protobuf/proto"
	google_protobuf "github.com/gogo/protobuf/types"
	"github.com/onsi/gomega"
	mcp "istio.io/api/mcp/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	mcpclient "istio.io/istio/galley/pkg/mcp/client"
	"istio.io/istio/pilot/pkg/config/memory"
	coredatamodel "istio.io/istio/pilot/pkg/mcp"
	"istio.io/istio/pilot/pkg/model"
)

func TestCoreDataModelUpdate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	store := memory.Make(model.IstioConfigTypes)
	istioConfigStore := model.MakeIstioStore(store)

	configDescriptor := model.ConfigDescriptor{
		model.VirtualService,
		model.Gateway,
		model.DestinationRule,
	}

	marshaledVirtualService, err := proto.Marshal(exampleVirtualService)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	marshaledGateway, err := proto.Marshal(exampleGateway)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	marshaledDestinationRule, err := proto.Marshal(exampleDestinationRule)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	allResources := map[string][]byte{
		model.VirtualService.Type:  marshaledVirtualService,
		model.Gateway.Type:         marshaledGateway,
		model.DestinationRule.Type: marshaledDestinationRule,
	}

	for _, desc := range configDescriptor {
		value, _ := allResources[desc.Type]
		responseMessageName := desc.MessageName
		message, err := makeMessage(value, responseMessageName)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		change := makeChange(message, "unique-name", responseMessageName)

		updater := coredatamodel.NewUpdater(istioConfigStore, configDescriptor)
		err = updater.Update(change)
		g.Expect(err).ToNot(gomega.HaveOccurred())
	}

	t.Run("Config creation", func(t *testing.T) {
		configs, _ := istioConfigStore.List(model.Gateway.Type, "")
		g.Expect(len(configs)).To(gomega.Equal(1))

		updatedConfig := configs[0]
		g.Expect(updatedConfig.ConfigMeta.Type).To(gomega.Equal(model.Gateway.Type))
		g.Expect(updatedConfig.ConfigMeta.Group).To(gomega.Equal(model.Gateway.Group))
		g.Expect(updatedConfig.ConfigMeta.Version).To(gomega.Equal(model.Gateway.Version))
		g.Expect(updatedConfig.ConfigMeta.Name).To(gomega.Equal("unique-name"))
		g.Expect(updatedConfig.ConfigMeta.Namespace).To(gomega.Equal(""))
		g.Expect(updatedConfig.ConfigMeta.Labels).To(gomega.Equal(map[string]string{}))
		g.Expect(updatedConfig.ConfigMeta.Annotations).To(gomega.Equal(map[string]string{}))
		g.Expect(updatedConfig.Spec).To(gomega.Equal(exampleGateway))

		configs, _ = istioConfigStore.List(model.DestinationRule.Type, "")
		g.Expect(len(configs)).To(gomega.Equal(1))

		updatedConfig = configs[0]
		g.Expect(updatedConfig.ConfigMeta.Type).To(gomega.Equal(model.DestinationRule.Type))
		g.Expect(updatedConfig.ConfigMeta.Group).To(gomega.Equal(model.DestinationRule.Group))
		g.Expect(updatedConfig.ConfigMeta.Version).To(gomega.Equal(model.DestinationRule.Version))
		g.Expect(updatedConfig.ConfigMeta.Name).To(gomega.Equal("unique-name"))
		g.Expect(updatedConfig.ConfigMeta.Namespace).To(gomega.Equal(""))
		g.Expect(updatedConfig.ConfigMeta.Labels).To(gomega.Equal(map[string]string{}))
		g.Expect(updatedConfig.ConfigMeta.Annotations).To(gomega.Equal(map[string]string{}))
		g.Expect(updatedConfig.Spec).To(gomega.Equal(exampleDestinationRule))

		configs, _ = istioConfigStore.List(model.VirtualService.Type, "")
		g.Expect(len(configs)).To(gomega.Equal(1))

		updatedConfig = configs[0]
		g.Expect(updatedConfig.ConfigMeta.Type).To(gomega.Equal(model.VirtualService.Type))
		g.Expect(updatedConfig.ConfigMeta.Group).To(gomega.Equal(model.VirtualService.Group))
		g.Expect(updatedConfig.ConfigMeta.Version).To(gomega.Equal(model.VirtualService.Version))
		g.Expect(updatedConfig.ConfigMeta.Name).To(gomega.Equal("unique-name"))
		g.Expect(updatedConfig.ConfigMeta.Namespace).To(gomega.Equal(""))
		g.Expect(updatedConfig.ConfigMeta.Labels).To(gomega.Equal(map[string]string{}))
		g.Expect(updatedConfig.ConfigMeta.Annotations).To(gomega.Equal(map[string]string{}))
		g.Expect(updatedConfig.Spec).To(gomega.Equal(exampleVirtualService))
	})

	t.Run("Config update", func(t *testing.T) {
		newGateway := &networking.Gateway{
			Servers: []*networking.Server{
				&networking.Server{
					Port: &networking.Port{
						Name:     "tcp",
						Number:   566,
						Protocol: "TCP",
					},
					Hosts: []string{
						"*",
					},
				},
			},
		}

		marshaledNewGateway, err := proto.Marshal(newGateway)
		g.Expect(err).ToNot(gomega.HaveOccurred())

		responseMessageName := model.Gateway.MessageName
		message, err := makeMessage(marshaledNewGateway, responseMessageName)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		change := makeChange(message, "unique-name", responseMessageName)

		updater := coredatamodel.NewUpdater(istioConfigStore, configDescriptor)
		err = updater.Update(change)
		g.Expect(err).ToNot(gomega.HaveOccurred())

		configs, _ := istioConfigStore.List(model.Gateway.Type, "")
		g.Expect(len(configs)).To(gomega.Equal(1))

		updatedConfig := configs[0]
		g.Expect(updatedConfig.ConfigMeta.Type).To(gomega.Equal(model.Gateway.Type))
		g.Expect(updatedConfig.ConfigMeta.Group).To(gomega.Equal(model.Gateway.Group))
		g.Expect(updatedConfig.ConfigMeta.Version).To(gomega.Equal(model.Gateway.Version))
		g.Expect(updatedConfig.ConfigMeta.Name).To(gomega.Equal("unique-name"))
		g.Expect(updatedConfig.ConfigMeta.Namespace).To(gomega.Equal(""))
		g.Expect(updatedConfig.ConfigMeta.Labels).To(gomega.Equal(map[string]string{}))
		g.Expect(updatedConfig.ConfigMeta.Annotations).To(gomega.Equal(map[string]string{}))
		g.Expect(updatedConfig.Spec).To(gomega.Equal(newGateway))

		configs, _ = istioConfigStore.List(model.DestinationRule.Type, "")
		g.Expect(len(configs)).To(gomega.Equal(1))

		configs, _ = istioConfigStore.List(model.VirtualService.Type, "")
		g.Expect(len(configs)).To(gomega.Equal(1))
	})
}

func makeMessage(value []byte, responseMessageName string) (proto.Message, error) {
	resource := &google_protobuf.Any{
		TypeUrl: fmt.Sprintf("type.googleapis.com/%s", responseMessageName),
		Value:   value,
	}

	var dynamicAny google_protobuf.DynamicAny
	err := google_protobuf.UnmarshalAny(resource, &dynamicAny)
	if err == nil {
		return dynamicAny.Message, nil
	}

	return nil, err
}

func makeChange(resource proto.Message, name, responseMessageName string) *mcpclient.Change {
	return &mcpclient.Change{
		MessageName: responseMessageName,
		Objects: []*mcpclient.Object{
			{
				MessageName: responseMessageName,
				Metadata: &mcp.Metadata{
					Name: name,
				},
				Resource: resource,
			},
		},
	}
}

var exampleGateway = &networking.Gateway{
	Servers: []*networking.Server{
		&networking.Server{
			Port: &networking.Port{
				Name:     "http",
				Number:   80,
				Protocol: "HTTP",
			},
			Hosts: []string{
				"*",
			},
		},
	},
}

var exampleVirtualService = &networking.VirtualService{
	Hosts: []string{"prod", "test"},
	Http: []*networking.HTTPRoute{
		{
			Route: []*networking.DestinationWeight{
				{
					Destination: &networking.Destination{
						Host: "job",
					},
					Weight: 80,
				},
			},
		},
	},
}

var exampleDestinationRule = &networking.DestinationRule{
	Host: "ratings",
	TrafficPolicy: &networking.TrafficPolicy{
		LoadBalancer: &networking.LoadBalancerSettings{
			new(networking.LoadBalancerSettings_Simple),
		},
	},
}
