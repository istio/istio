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
	coredatamodel "istio.io/istio/pilot/pkg/config/mcp"
	"istio.io/istio/pilot/pkg/config/mcp/fakes"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/model"
)

func TestCoreDataModelUpdate(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	configDescriptor := model.ConfigDescriptor{
		model.VirtualService,
		model.Gateway,
		model.DestinationRule,
	}
	store := memory.Make(configDescriptor)
	expectedGateway := model.Config{
		ConfigMeta: model.ConfigMeta{
			Name: "some-gateway",
			Type: "gateway",
		},
		Spec: exampleGateway,
	}

	store.Create(expectedGateway)
	logger := &fakes.Logger{}
	controller := coredatamodel.NewController(store, logger)
	updater := coredatamodel.NewUpdater(controller)

	updatedGateway := &networking.Gateway{
		Servers: []*networking.Server{
			&networking.Server{
				Port: &networking.Port{
					Name:     "https",
					Number:   443,
					Protocol: "HTTP",
				},
				Hosts: []string{
					"*",
				},
			},
		},
	}

	marshaledGateway, err := proto.Marshal(updatedGateway)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	message, err := makeMessage(marshaledGateway, model.Gateway.MessageName)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	change := makeChange(message, "some-gateway", model.Gateway.MessageName)
	err = updater.Update(change)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	c, exist := controller.Get("gateway", "some-gateway", "")
	g.Expect(exist).To(gomega.BeTrue())
	g.Expect(c.Name).To(gomega.Equal(expectedGateway.Name))
	g.Expect(c.Type).To(gomega.Equal(expectedGateway.Type))
	g.Expect(c.Spec).To(gomega.Equal(updatedGateway))

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
