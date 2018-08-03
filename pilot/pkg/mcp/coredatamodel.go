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
package coredatamodel

import (
	mcpclient "istio.io/istio/galley/pkg/mcp/client"
	"istio.io/istio/pilot/pkg/model"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Updater struct {
	CoreDataModel
	configStore      model.ConfigStore
	configDescriptor model.ConfigDescriptor
}

type CoreDataModel interface {
	model.ConfigStore
	// TODO: ServiceDiscovery
}

func NewUpdater(store model.ConfigStore, configDescriptor model.ConfigDescriptor) *Updater {
	return &Updater{
		configStore:      store,
		configDescriptor: configDescriptor,
	}
}

func (u *Updater) Update(change *mcpclient.Change) error {
	for _, obj := range change.Objects {
		for _, descriptor := range u.configDescriptor {
			if descriptor.MessageName == change.MessageName {
				c, exists := u.configStore.Get(descriptor.Type, obj.Metadata.Name, "")
				if exists {
					c.Spec = obj.Resource
					_, err := u.configStore.Update(*c)
					return err
				}
				config := model.Config{
					ConfigMeta: model.ConfigMeta{
						Type:              descriptor.Type,
						Group:             descriptor.Group,
						Version:           descriptor.Version,
						Name:              obj.Metadata.Name,
						Namespace:         "",
						Domain:            "",
						Labels:            map[string]string{},
						Annotations:       map[string]string{},
						ResourceVersion:   "",
						CreationTimestamp: meta_v1.Time{},
					},
					Spec: obj.Resource,
				}
				_, err := u.configStore.Create(config)
				return err
			}
		}
	}

	return nil
}
