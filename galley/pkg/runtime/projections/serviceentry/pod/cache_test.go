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
	"reflect"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/metadata"
	"istio.io/istio/galley/pkg/runtime/processing"
	"istio.io/istio/galley/pkg/runtime/projections/serviceentry/pod"
	"istio.io/istio/galley/pkg/runtime/resource"

	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubernetes/pkg/kubelet/apis"
)

const (
	ip                         = "1.2.3.4"
	nodeName                   = "node1"
	podName                    = "pod1"
	namespace                  = "ns"
	region                     = "region1"
	zone                       = "zone1"
	expectedLocality           = "region1/zone1"
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

	labels = map[string]string{
		"l1": "v1",
		"l2": "v2",
	}
)

func TestPodLifecycle(t *testing.T) {
	l := &listener{}
	c, h := pod.NewCache(l.asListener())

	labels := map[string]string{
		"l2": "v2",
	}

	// Add the node.
	h.Handle(resource.Event{
		Kind:  resource.Added,
		Entry: nodeEntry(region, zone),
	})

	t.Run("Add", func(t *testing.T) {
		g := NewGomegaWithT(t)
		h.Handle(resource.Event{
			Kind: resource.Added,
			Entry: newPodEntryBuilder().
				IP(ip).
				Labels(labels).
				Phase(coreV1.PodPending).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		})
		p, _ := c.GetPodByIP(ip)
		expected := pod.Info{
			FullName:           fullName,
			IP:                 ip,
			Locality:           expectedLocality,
			NodeName:           nodeName,
			ServiceAccountName: expectedServiceAccountName,
			Labels:             labels,
		}
		g.Expect(p).To(Equal(expected))
		l.assertAdded(t, expected)
	})

	l.reset()

	t.Run("NoChange", func(t *testing.T) {
		g := NewGomegaWithT(t)
		h.Handle(resource.Event{
			Kind: resource.Updated,
			Entry: newPodEntryBuilder().
				IP(ip).
				Labels(labels).
				Phase(coreV1.PodRunning).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		})
		p, _ := c.GetPodByIP(ip)
		expected := pod.Info{
			FullName:           fullName,
			IP:                 ip,
			Locality:           expectedLocality,
			NodeName:           nodeName,
			ServiceAccountName: expectedServiceAccountName,
			Labels:             labels,
		}
		g.Expect(p).To(Equal(expected))
		l.assertNone(t)
	})

	l.reset()

	t.Run("ChangeLabel", func(t *testing.T) {
		g := NewGomegaWithT(t)

		labels = map[string]string{
			"l3": "v3",
			"l4": "v4",
		}
		h.Handle(resource.Event{
			Kind: resource.Updated,
			Entry: newPodEntryBuilder().
				IP(ip).
				Labels(labels).
				Phase(coreV1.PodRunning).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		})
		p, _ := c.GetPodByIP(ip)
		expected := pod.Info{
			FullName:           fullName,
			IP:                 ip,
			Locality:           expectedLocality,
			NodeName:           nodeName,
			ServiceAccountName: expectedServiceAccountName,
			Labels:             labels,
		}
		g.Expect(p).To(Equal(expected))
		l.assertUpdated(t, expected)
	})

	l.reset()

	t.Run("Delete", func(t *testing.T) {
		g := NewGomegaWithT(t)
		h.Handle(resource.Event{
			Kind: resource.Deleted,
			Entry: newPodEntryBuilder().
				IP(ip).
				Labels(labels).
				Phase(coreV1.PodRunning).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		})
		_, ok := c.GetPodByIP(ip)
		g.Expect(ok).To(BeFalse())
		l.assertDeleted(t, pod.Info{
			FullName:           fullName,
			IP:                 ip,
			Locality:           expectedLocality,
			NodeName:           nodeName,
			ServiceAccountName: expectedServiceAccountName,
			Labels:             labels,
		})
	})
}

func TestNodeLifecycle(t *testing.T) {
	g := NewGomegaWithT(t)

	l := &listener{}
	c, h := pod.NewCache(l.asListener())

	applyEvents(l, h, []resource.Event{
		{
			Kind:  resource.Added,
			Entry: nodeEntry(region, zone),
		},
		{
			Kind: resource.Added,
			Entry: newPodEntryBuilder().
				IP(ip).
				Labels(labels).
				Phase(coreV1.PodPending).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		},
		{
			Kind:  resource.Deleted,
			Entry: nodeEntry(region, zone),
		},
	})

	p, _ := c.GetPodByIP(ip)
	expected := pod.Info{
		FullName:           fullName,
		IP:                 ip,
		Locality:           "",
		NodeName:           nodeName,
		ServiceAccountName: expectedServiceAccountName,
		Labels:             labels,
	}
	g.Expect(p).To(Equal(expected))
	l.assertUpdated(t, expected)
}

func TestNodeAddedAfterPod(t *testing.T) {
	g := NewGomegaWithT(t)

	l := &listener{}
	c, h := pod.NewCache(l.asListener())

	applyEvents(l, h, []resource.Event{
		{
			Kind: resource.Added,
			Entry: newPodEntryBuilder().
				IP(ip).
				Labels(labels).
				Phase(coreV1.PodPending).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		},
		{
			Kind:  resource.Added,
			Entry: nodeEntry(region, zone),
		},
	})

	p, _ := c.GetPodByIP(ip)
	expected := pod.Info{
		FullName:           fullName,
		IP:                 ip,
		Locality:           expectedLocality,
		NodeName:           nodeName,
		ServiceAccountName: expectedServiceAccountName,
		Labels:             labels,
	}
	g.Expect(p).To(Equal(expected))
	l.assertUpdated(t, expected)
}

func TestNodeWithOnlyRegion(t *testing.T) {
	g := NewGomegaWithT(t)

	l := &listener{}
	c, h := pod.NewCache(l.asListener())

	applyEvents(l, h, []resource.Event{
		{
			Kind:  resource.Added,
			Entry: nodeEntry(region, ""),
		},
		{
			Kind: resource.Added,
			Entry: newPodEntryBuilder().
				IP(ip).
				Phase(coreV1.PodPending).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		},
	})

	p, _ := c.GetPodByIP(ip)
	expected := pod.Info{
		FullName:           fullName,
		IP:                 ip,
		Locality:           "region1/",
		NodeName:           nodeName,
		ServiceAccountName: expectedServiceAccountName,
	}
	g.Expect(p).To(Equal(expected))
	l.assertAdded(t, expected)
}

func TestNodeWithNoLocality(t *testing.T) {
	g := NewGomegaWithT(t)

	l := &listener{}
	c, h := pod.NewCache(l.asListener())

	applyEvents(l, h, []resource.Event{
		{
			Kind:  resource.Added,
			Entry: nodeEntry("", ""),
		},
		{
			Kind: resource.Added,
			Entry: newPodEntryBuilder().
				IP(ip).
				Phase(coreV1.PodPending).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		},
	})

	p, _ := c.GetPodByIP(ip)
	expected := pod.Info{
		FullName:           fullName,
		IP:                 ip,
		Locality:           "",
		NodeName:           nodeName,
		ServiceAccountName: expectedServiceAccountName,
	}
	g.Expect(p).To(Equal(expected))
	l.assertAdded(t, expected)
}

func TestNoNamespaceAndNoServiceAccount(t *testing.T) {
	l := &listener{}
	c, h := pod.NewCache(l.asListener())

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
	p, _ := c.GetPodByIP(ip)
	expected := pod.Info{
		IP:                 ip,
		FullName:           fullName,
		NodeName:           nodeName,
		ServiceAccountName: "spiffe://cluster.local/ns//sa/",
	}
	g.Expect(p).To(Equal(expected))
	l.assertAdded(t, expected)
}

func TestWrongCollectionShouldNotPanic(t *testing.T) {
	l := &listener{}
	_, h := pod.NewCache(l.asListener())

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
	l.assertNone(t)
}

func TestInvalidPodPhase(t *testing.T) {
	l := &listener{}
	c, h := pod.NewCache(l.asListener())

	for _, phase := range []coreV1.PodPhase{coreV1.PodSucceeded, coreV1.PodFailed, coreV1.PodUnknown} {
		t.Run(string(phase), func(t *testing.T) {
			g := NewGomegaWithT(t)
			h.Handle(resource.Event{
				Kind: resource.Added,
				Entry: newPodEntryBuilder().
					IP(ip).
					Labels(labels).
					Phase(phase).
					NodeName(nodeName).
					ServiceAccountName(serviceAccountName).Build(),
			})
			_, ok := c.GetPodByIP(ip)
			g.Expect(ok).To(BeFalse())
		})
	}
}

func TestUpdateWithInvalidPhaseShouldDelete(t *testing.T) {
	g := NewGomegaWithT(t)

	l := &listener{}
	c, h := pod.NewCache(l.asListener())

	applyEvents(l, h, []resource.Event{
		{
			Kind: resource.Added,
			Entry: newPodEntryBuilder().
				IP(ip).
				Labels(labels).
				Phase(coreV1.PodPending).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		},
		{
			Kind: resource.Updated,
			Entry: newPodEntryBuilder().
				IP(ip).
				Labels(labels).
				Phase(coreV1.PodUnknown).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		},
	})

	_, ok := c.GetPodByIP(ip)
	g.Expect(ok).To(BeFalse())
	l.assertDeleted(t, pod.Info{
		IP:                 ip,
		FullName:           fullName,
		NodeName:           nodeName,
		Labels:             labels,
		ServiceAccountName: expectedServiceAccountName,
	})
}

func TestDeleteWithNoItemShouldUseFullName(t *testing.T) {
	g := NewGomegaWithT(t)

	l := &listener{}
	c, h := pod.NewCache(l.asListener())

	applyEvents(l, h, []resource.Event{
		{
			Kind: resource.Added,
			Entry: newPodEntryBuilder().
				IP(ip).
				Labels(labels).
				Phase(coreV1.PodPending).
				NodeName(nodeName).
				ServiceAccountName(serviceAccountName).Build(),
		},
		{
			Kind: resource.Deleted,
			Entry: resource.Entry{
				ID: id,
			},
		},
	})

	_, ok := c.GetPodByIP(ip)
	g.Expect(ok).To(BeFalse())
}

func TestDeleteNotFoundShouldNotPanic(t *testing.T) {
	l := &listener{}
	_, h := pod.NewCache(l.asListener())

	// Delete it, but with a nil Item to force a lookup by fullName.
	h.Handle(resource.Event{
		Kind: resource.Deleted,
		Entry: newPodEntryBuilder().
			IP(ip).
			Labels(labels).
			Phase(coreV1.PodPending).
			NodeName(nodeName).
			ServiceAccountName(serviceAccountName).Build(),
	})
}

func TestDeleteNotFoundWithMissingItemShouldNotPanic(t *testing.T) {
	l := &listener{}
	_, h := pod.NewCache(l.asListener())

	// Delete it, but with a nil Item to force a lookup by fullName.
	h.Handle(resource.Event{
		Kind: resource.Deleted,
		Entry: resource.Entry{
			ID: id,
		},
	})
}

func TestPodWithNoIPShouldBeIgnored(t *testing.T) {
	l := &listener{}
	_, h := pod.NewCache(l.asListener())

	h.Handle(resource.Event{
		Kind: resource.Added,
		Entry: newPodEntryBuilder().
			Phase(coreV1.PodPending).Build(),
	})
	l.assertNone(t)
}

func applyEvents(l *listener, h processing.Handler, events []resource.Event) {
	for _, event := range events {
		l.reset()
		h.Handle(event)
	}
}

type podEntryBuilder struct {
	ip                 string
	nodeName           string
	labels             map[string]string
	serviceAccountName string
	phase              coreV1.PodPhase
}

func newPodEntryBuilder() *podEntryBuilder {
	return &podEntryBuilder{}
}

func (b *podEntryBuilder) IP(ip string) *podEntryBuilder {
	b.ip = ip
	return b
}

func (b *podEntryBuilder) NodeName(nodeName string) *podEntryBuilder {
	b.nodeName = nodeName
	return b
}

func (b *podEntryBuilder) Labels(labels map[string]string) *podEntryBuilder {
	b.labels = labels
	return b
}

func (b *podEntryBuilder) ServiceAccountName(serviceAccountName string) *podEntryBuilder {
	b.serviceAccountName = serviceAccountName
	return b
}

func (b *podEntryBuilder) Phase(phase coreV1.PodPhase) *podEntryBuilder {
	b.phase = phase
	return b
}

func (b *podEntryBuilder) Build() resource.Entry {
	return resource.Entry{
		ID: id,
		Item: &coreV1.Pod{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				Labels:    b.labels,
			},
			Spec: coreV1.PodSpec{
				NodeName:           b.nodeName,
				ServiceAccountName: b.serviceAccountName,
			},
			Status: coreV1.PodStatus{
				PodIP: b.ip,
				Phase: b.phase,
			},
		},
	}
}

func nodeEntry(region, zone string) resource.Entry {
	labels := make(resource.Labels)
	if region != "" {
		labels[apis.LabelZoneRegion] = region
	}
	if zone != "" {
		labels[apis.LabelZoneFailureDomain] = zone
	}
	return resource.Entry{
		ID: resource.VersionedKey{
			Key: resource.Key{
				Collection: metadata.K8sCoreV1Nodes.Collection,
				FullName:   resource.FullNameFromNamespaceAndName("", nodeName),
			},
		},
		Metadata: resource.Metadata{
			Labels: labels,
		},
	}
}

type listener struct {
	added   []pod.Info
	updated []pod.Info
	deleted []pod.Info
}

func (l *listener) reset() {
	l.added = l.added[:0]
	l.updated = l.updated[:0]
	l.deleted = l.deleted[:0]
}

func (l *listener) onAdded(p pod.Info) {
	l.added = append(l.added, p)
}

func (l *listener) onUpdated(p pod.Info) {
	l.updated = append(l.updated, p)
}

func (l *listener) onDeleted(p pod.Info) {
	l.deleted = append(l.deleted, p)
}

func (l *listener) asListener() pod.Listener {
	return pod.Listener{
		PodAdded:   l.onAdded,
		PodUpdated: l.onUpdated,
		PodDeleted: l.onDeleted,
	}
}

func (l *listener) assertNone(t *testing.T) {
	t.Helper()
	assertNone(t, "added", l.added)
	assertNone(t, "updated", l.updated)
	assertNone(t, "deleted", l.deleted)
}

func (l *listener) assertAdded(t *testing.T, expected pod.Info) {
	t.Helper()
	assertOne(t, "added", l.added, expected)
	assertNone(t, "updated", l.updated)
	assertNone(t, "deleted", l.deleted)

}

func (l *listener) assertUpdated(t *testing.T, expected pod.Info) {
	t.Helper()
	assertNone(t, "added", l.added)
	assertOne(t, "updated", l.updated, expected)
	assertNone(t, "deleted", l.deleted)

}

func (l *listener) assertDeleted(t *testing.T, expected pod.Info) {
	t.Helper()
	assertNone(t, "added", l.added)
	assertNone(t, "updated", l.updated)
	assertOne(t, "deleted", l.deleted, expected)
}

func assertNone(t *testing.T, name string, result []pod.Info) {
	t.Helper()
	if len(result) > 0 {
		t.Fatalf("%s: expected 0, found %d", name, len(result))
	}
}

func assertOne(t *testing.T, name string, result []pod.Info, expected pod.Info) {
	t.Helper()
	if len(result) != 1 {
		t.Fatalf("%s: expected 1, found %d", name, len(result))
	}
	actual := result[0]
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("%s: expected\n%v+\nto equal\n%v+", name, actual, expected)
	}
}
