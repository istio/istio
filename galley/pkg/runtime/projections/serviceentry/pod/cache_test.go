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

package pod_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/pod"
	"istio.io/istio/galley/pkg/runtime/resource"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	podName                    = "pod1"
	nodeName                   = "node1"
	namespace                  = "ns"
	serviceAccountName         = "myServiceAccount"
	expectedServiceAccountName = "spiffe://cluster.local/ns/ns/sa/myServiceAccount"
)

var (
	fullName = resource.FullNameFromNamespaceAndName(namespace, podName)

	id = resource.VersionedKey{
		Key: resource.Key{
			Collection: metadata.K8sCoreV1Pods.Collection,
			FullName:   fullName,
		},
	}
)

func TestBasicEvents(t *testing.T) {
	c, h := pod.NewCache()

	t.Run("Add", func(t *testing.T) {
		g := NewGomegaWithT(t)
		h.Handle(resource.Event{
			Kind:  resource.Added,
			Entry: entry("1.2.3.4", coreV1.PodPending),
		})
		p, _ := c.GetPodByIP("1.2.3.4")
		g.Expect(p).To(Equal(pod.Info{
			FullName:           fullName,
			NodeName:           nodeName,
			ServiceAccountName: expectedServiceAccountName,
		}))
	})

	t.Run("Update", func(t *testing.T) {
		g := NewGomegaWithT(t)
		h.Handle(resource.Event{
			Kind:  resource.Updated,
			Entry: entry("1.2.3.4", coreV1.PodRunning),
		})
		p, _ := c.GetPodByIP("1.2.3.4")
		g.Expect(p).To(Equal(pod.Info{
			FullName:           fullName,
			NodeName:           nodeName,
			ServiceAccountName: expectedServiceAccountName,
		}))
	})

	t.Run("Delete", func(t *testing.T) {
		g := NewGomegaWithT(t)
		h.Handle(resource.Event{
			Kind:  resource.Deleted,
			Entry: entry("1.2.3.4", coreV1.PodRunning),
		})
		_, ok := c.GetPodByIP("1.2.3.4")
		g.Expect(ok).To(BeFalse())
	})
}

func TestNoNamespaceAndNoServiceAccount(t *testing.T) {
	c, h := pod.NewCache()
	g := NewGomegaWithT(t)
	h.Handle(resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: id,
			Item: &coreV1.Pod{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      podName,
					Namespace: "",
				},
				Spec: coreV1.PodSpec{
					NodeName:           nodeName,
					ServiceAccountName: "",
				},
				Status: coreV1.PodStatus{
					PodIP: "1.2.3.4",
					Phase: coreV1.PodRunning,
				},
			},
		},
	})
	p, _ := c.GetPodByIP("1.2.3.4")
	g.Expect(p).To(Equal(pod.Info{
		FullName:           fullName,
		NodeName:           nodeName,
		ServiceAccountName: "spiffe://cluster.local/ns//sa/",
	}))
}

func TestWrongCollectionShouldNotPanic(t *testing.T) {
	_, h := pod.NewCache()
	h.Handle(resource.Event{
		Kind: resource.Added,
		Entry: resource.Entry{
			ID: resource.VersionedKey{
				Key: resource.Key{
					Collection: metadata.K8sCoreV1Services.Collection,
					FullName:   resource.FullNameFromNamespaceAndName("ns", "myservice"),
				},
			},
			Item: &coreV1.Service{},
		},
	})
}

func TestInvalidPodPhase(t *testing.T) {
	c, h := pod.NewCache()

	for _, phase := range []coreV1.PodPhase{coreV1.PodSucceeded, coreV1.PodFailed, coreV1.PodUnknown} {
		t.Run(string(phase), func(t *testing.T) {
			g := NewGomegaWithT(t)
			h.Handle(resource.Event{
				Kind:  resource.Added,
				Entry: entry("1.2.3.4", phase),
			})
			_, ok := c.GetPodByIP("1.2.3.4")
			g.Expect(ok).To(BeFalse())
		})
	}
}

func TestUpdateWithInvalidPhaseShouldDelete(t *testing.T) {
	g := NewGomegaWithT(t)

	c, h := pod.NewCache()

	// Add it.
	h.Handle(resource.Event{
		Kind:  resource.Added,
		Entry: entry("1.2.3.4", coreV1.PodPending),
	})
	_, ok := c.GetPodByIP("1.2.3.4")
	g.Expect(ok).ToNot(BeFalse())

	h.Handle(resource.Event{
		Kind:  resource.Updated,
		Entry: entry("1.2.3.4", coreV1.PodUnknown),
	})
	_, ok = c.GetPodByIP("1.2.3.4")
	g.Expect(ok).To(BeFalse())
}

func TestDeleteWithNoItemShouldUseFullName(t *testing.T) {
	g := NewGomegaWithT(t)

	c, h := pod.NewCache()

	// Add it.
	h.Handle(resource.Event{
		Kind:  resource.Added,
		Entry: entry("1.2.3.4", coreV1.PodPending),
	})
	_, ok := c.GetPodByIP("1.2.3.4")
	g.Expect(ok).To(BeTrue())

	// Delete it, but with a nil Item to force a lookup by fullName.
	h.Handle(resource.Event{
		Kind: resource.Deleted,
		Entry: resource.Entry{
			ID: id,
		},
	})
	_, ok = c.GetPodByIP("1.2.3.4")
	g.Expect(ok).To(BeFalse())
}

func TestDeleteNotFoundShouldNotPanic(t *testing.T) {
	_, h := pod.NewCache()

	// Delete it, but with a nil Item to force a lookup by fullName.
	h.Handle(resource.Event{
		Kind:  resource.Deleted,
		Entry: entry("1.2.3.4", coreV1.PodRunning),
	})
}

func TestDeleteNotFoundWithMissingItemShouldNotPanic(t *testing.T) {
	_, h := pod.NewCache()

	// Delete it, but with a nil Item to force a lookup by fullName.
	h.Handle(resource.Event{
		Kind: resource.Deleted,
		Entry: resource.Entry{
			ID: id,
		},
	})
}

func TestNoIP(t *testing.T) {
	g := NewGomegaWithT(t)

	c, h := pod.NewCache()

	h.Handle(resource.Event{
		Kind:  resource.Added,
		Entry: entry("", coreV1.PodRunning),
	})
	_, ok := c.GetPodByIP("")
	g.Expect(ok).To(BeFalse())
}

func entry(ip string, phase coreV1.PodPhase) resource.Entry {
	return resource.Entry{
		ID: id,
		Item: &coreV1.Pod{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
			},
			Spec: coreV1.PodSpec{
				NodeName:           nodeName,
				ServiceAccountName: serviceAccountName,
			},
			Status: coreV1.PodStatus{
				PodIP: ip,
				Phase: phase,
			},
		},
	}
}
