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
	"fmt"

	mcpclient "istio.io/istio/galley/pkg/mcp/client"
	"istio.io/istio/pilot/pkg/model"
)

type Updater struct {
	controller  model.ConfigStoreCache
	descriptors map[string]model.ProtoSchema
}

func NewUpdater(c model.ConfigStoreCache) *Updater {
	descriptors := make(map[string]model.ProtoSchema, len(c.ConfigDescriptor()))
	for _, config := range c.ConfigDescriptor() {
		descriptors[config.MessageName] = config
	}
	return &Updater{
		controller:  c,
		descriptors: descriptors,
	}
}

func (u *Updater) Update(change *mcpclient.Change) error {
	for _, obj := range change.Objects {
		descriptor, ok := u.descriptors[change.MessageName]
		if !ok {
			return fmt.Errorf("unknown type: %s received by updater", change.MessageName)
		}
		c, exists := u.controller.Get(descriptor.Type, obj.Metadata.Name, "")
		if exists {
			c.Spec = obj.Resource
			_, err := u.controller.Update(*c)
			return err
		}
	}
	return nil
}
