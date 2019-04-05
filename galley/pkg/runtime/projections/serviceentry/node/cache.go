// Copyright 2019 Istio Authors
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

package node

import (
	"fmt"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/resource"

	"k8s.io/kubernetes/pkg/kubelet/apis"
)

var _ Cache = cacheImpl{}
var _ processing.Handler = cacheImpl{}

// Info for a node.
type Info struct {
	Locality string
}

// Cache for pod Info.
type Cache interface {
	GetNodeByName(name string) (Info, bool)
}

// NewCache creates a cache and its update handler.
func NewCache() (Cache, processing.Handler) {
	c := make(cacheImpl)
	return c, c
}

type cacheImpl map[string]Info

func (nc cacheImpl) GetNodeByName(name string) (Info, bool) {
	node, ok := nc[name]
	return node, ok
}

func (nc cacheImpl) Handle(event resource.Event) {
	if event.Entry.ID.Collection != metadata.K8sCoreV1Nodes.Collection {
		return
	}

	// Nodes don't have namespaces.
	_, name := event.Entry.ID.FullName.InterpretAsNamespaceAndName()

	switch event.Kind {
	case resource.Added, resource.Updated:
		// Just update the node information directly
		labels := event.Entry.Metadata.Labels

		region := labels[apis.LabelZoneRegion]
		zone := labels[apis.LabelZoneFailureDomain]
		nc[name] = Info{
			Locality: getLocality(region, zone),
		}
	case resource.Deleted:
		delete(nc, name)
	}
}

func getLocality(region, zone string) string {
	if region == "" && zone == "" {
		return ""
	}

	return fmt.Sprintf("%v/%v", region, zone)
}
