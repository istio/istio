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

package node_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/node"
	"istio.io/istio/galley/pkg/runtime/resource"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/kubelet/apis"
)

const (
	name = "node1"
)

var (
	id = resource.VersionedKey{
		Key: resource.Key{
			Collection: metadata.K8sCoreV1Nodes.Collection,
			FullName:   resource.FullNameFromNamespaceAndName("", name),
		},
	}
)

func TestBasicEvents(t *testing.T) {
	c, h := node.NewCache()

	t.Run("Add", func(t *testing.T) {
		g := NewGomegaWithT(t)
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: entry("region1", "zone1"),
		})
		n, ok := c.GetNodeByName(name)
		g.Expect(ok).To(BeTrue())
		g.Expect(n.Locality).To(Equal("region1/zone1"))
	})

	t.Run("Update", func(t *testing.T) {
		g := NewGomegaWithT(t)
		h.Handle(resource.Event{
			Kind:  resource.Updated,
			Entry: entry("region1", "zone2"),
		})
		n, ok := c.GetNodeByName(name)
		g.Expect(ok).To(BeTrue())
		g.Expect(n.Locality).To(Equal("region1/zone2"))
	})

	t.Run("Delete", func(t *testing.T) {
		g := NewGomegaWithT(t)
		h.Handle(resource.Event{
			Kind:  resource.Deleted,
			Entry: entry("region1", "zone2"),
		})
		_, ok := c.GetNodeByName(name)
		g.Expect(ok).To(BeFalse())
	})
}

func TestWrongCollectionShouldNotPanic(t *testing.T) {
	_, h := node.NewCache()
	h.Handle(resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Key: resource.Key{
					Collection: metadata.K8sCoreV1Services.Collection,
					FullName:   resource.FullNameFromNamespaceAndName("ns", name),
				},
			},
			Item: &coreV1.Service{},
		},
	})
}

func TestNodeWithOnlyRegion(t *testing.T) {
	g := NewGomegaWithT(t)

	c, h := node.NewCache()

	h.Handle(resource.Event{
		Kind:  resource.Added,
		Entry: entry("region1", ""),
	})

	n, ok := c.GetNodeByName(name)
	g.Expect(ok).To(BeTrue())
	g.Expect(n.Locality).To(Equal("region1/"))
}

func TestNodeWithNoLocality(t *testing.T) {
	g := NewGomegaWithT(t)

	c, h := node.NewCache()

	h.Handle(resource.Event{
		Kind:  resource.Added,
		Entry: entry("", ""),
	})

	n, ok := c.GetNodeByName(name)
	g.Expect(ok).To(BeTrue())
	g.Expect(n.Locality).To(Equal(""))
}

func labels(region, zone string) resource.Labels {
	labels := make(resource.Labels)
	if region != "" {
		labels[apis.LabelZoneRegion] = region
	}
	if zone != "" {
		labels[apis.LabelZoneFailureDomain] = zone
	}
	return labels
}

func entry(region, zone string) resource.Entry {
	return resource.Entry{
		ID: id,
		Metadata: resource.Metadata{
			Labels: labels(region, zone),
		},
	}
}
