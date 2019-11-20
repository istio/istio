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

package inmemory

import (
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/event"
	"istio.io/istio/galley/pkg/config/resource"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/galley/pkg/config/testing/k8smeta"
	"istio.io/istio/galley/pkg/config/util/kubeyaml"
)

func TestKubeSource_ApplyContent(t *testing.T) {
	g := NewGomegaWithT(t)

	s, acc := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", data.YamlN1I1V1)
	g.Expect(err).To(BeNil())

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))

	actual := s.Get(data.Collection1).AllSorted()
	g.Expect(actual).To(HaveLen(1))

	g.Expect(actual[0].Metadata.Name).To(Equal(data.EntryN1I1V1.Metadata.Name))

	g.Expect(acc.Events()).To(HaveLen(2))
	g.Expect(acc.Events()[0].Kind).To(Equal(event.FullSync))
	g.Expect(acc.Events()[1].Kind).To(Equal(event.Added))
	g.Expect(acc.Events()[1].Entry.Metadata.Name).To(Equal(data.EntryN1I1V1.Metadata.Name))
}

func TestKubeSource_ApplyContent_BeforeStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s, acc := setupKubeSource()
	defer s.Stop()

	err := s.ApplyContent("foo", data.YamlN1I1V1)
	g.Expect(err).To(BeNil())
	s.Start()

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))

	actual := s.Get(data.Collection1).AllSorted()
	g.Expect(actual).To(HaveLen(1))

	g.Expect(actual[0].Metadata.Name).To(Equal(data.EntryN1I1V1.Metadata.Name))

	g.Expect(acc.Events()).To(HaveLen(2))
	g.Expect(acc.Events()[0].Kind).To(Equal(event.Added))
	g.Expect(acc.Events()[0].Entry.Metadata.Name).To(Equal(data.EntryN1I1V1.Metadata.Name))
	g.Expect(acc.Events()[1].Kind).To(Equal(event.FullSync))
}

func TestKubeSource_ApplyContent_Unchanged0Add1(t *testing.T) {
	g := NewGomegaWithT(t)

	s, acc := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlN2I2V1))
	g.Expect(err).To(BeNil())

	actual := s.Get(data.Collection1).AllSorted()
	g.Expect(actual).To(HaveLen(2))
	g.Expect(actual[0].Metadata.Name).To(Equal(data.EntryN1I1V1.Metadata.Name))
	g.Expect(actual[1].Metadata.Name).To(Equal(data.EntryN2I2V1.Metadata.Name))

	err = s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN2I2V2, data.YamlN3I3V1))
	g.Expect(err).To(BeNil())

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))

	actual = s.Get(data.Collection1).AllSorted()
	g.Expect(actual).To(HaveLen(2))
	g.Expect(actual[0].Metadata.Name).To(Equal(data.EntryN2I2V2.Metadata.Name))
	g.Expect(actual[1].Metadata.Name).To(Equal(data.EntryN3I3V1.Metadata.Name))

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(6))
	g.Expect(events[0].Kind).To(Equal(event.FullSync))
	g.Expect(events[1].Kind).To(Equal(event.Added))
	g.Expect(events[1].Entry).To(Equal(data.EntryN1I1V1))
	g.Expect(events[2].Kind).To(Equal(event.Added))
	g.Expect(events[2].Entry).To(Equal(withVersion(data.EntryN2I2V1, "v2")))
	g.Expect(events[3].Kind).To(Equal(event.Updated))
	g.Expect(events[3].Entry).To(Equal(withVersion(data.EntryN2I2V2, "v3")))
	g.Expect(events[4].Kind).To(Equal(event.Added))
	g.Expect(events[4].Entry).To(Equal(withVersion(data.EntryN3I3V1, "v4")))
	g.Expect(events[5].Kind).To(Equal(event.Deleted))
	g.Expect(events[5].Entry.Metadata.Name).To(Equal(data.EntryN1I1V1.Metadata.Name))
}

func TestKubeSource_RemoveContent(t *testing.T) {
	g := NewGomegaWithT(t)

	s, acc := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlN2I2V1))
	g.Expect(err).To(BeNil())
	err = s.ApplyContent("bar", kubeyaml.JoinString(data.YamlN3I3V1))
	g.Expect(err).To(BeNil())

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"bar": {}, "foo": {}}))

	s.RemoveContent("foo")
	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"bar": {}}))

	actual := s.Get(data.Collection1).AllSorted()
	g.Expect(actual).To(HaveLen(1))

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(6))
	g.Expect(events[0:4]).To(ConsistOf(
		event.FullSyncFor(data.Collection1),
		event.AddFor(data.Collection1, data.EntryN1I1V1),
		event.AddFor(data.Collection1, withVersion(data.EntryN2I2V1, "v2")),
		event.AddFor(data.Collection1, withVersion(data.EntryN3I3V1, "v3"))))

	//  Delete events can appear out of order.
	g.Expect(events[4].Kind).To(Equal(event.Deleted))
	g.Expect(events[5].Kind).To(Equal(event.Deleted))

	if events[4].Entry.Metadata.Name == data.EntryN1I1V1.Metadata.Name {
		g.Expect(events[4:]).To(ConsistOf(
			event.DeleteForResource(data.Collection1, data.EntryN1I1V1),
			event.DeleteForResource(data.Collection1, withVersion(data.EntryN2I2V1, "v2"))))
	} else {
		g.Expect(events[4:]).To(ConsistOf(
			event.DeleteForResource(data.Collection1, withVersion(data.EntryN2I2V1, "v2")),
			event.DeleteForResource(data.Collection1, data.EntryN1I1V1)))
	}
}

func TestKubeSource_Clear(t *testing.T) {
	g := NewGomegaWithT(t)

	s, acc := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlN2I2V1))
	g.Expect(err).To(BeNil())

	s.Clear()

	actual := s.Get(data.Collection1).AllSorted()
	g.Expect(actual).To(HaveLen(0))

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(5))
	g.Expect(events[0].Kind).To(Equal(event.FullSync))
	g.Expect(events[1].Kind).To(Equal(event.Added))
	g.Expect(events[1].Entry).To(Equal(data.EntryN1I1V1))
	g.Expect(events[2].Kind).To(Equal(event.Added))
	g.Expect(events[2].Entry).To(Equal(withVersion(data.EntryN2I2V1, "v2")))

	g.Expect(events[3].Kind).To(Equal(event.Deleted))
	g.Expect(events[4].Kind).To(Equal(event.Deleted))

	if events[3].Entry.Metadata.Name == data.EntryN1I1V1.Metadata.Name {
		g.Expect(events[3].Entry.Metadata.Name).To(Equal(data.EntryN1I1V1.Metadata.Name))
		g.Expect(events[4].Entry.Metadata.Name).To(Equal(data.EntryN2I2V1.Metadata.Name))
	} else {
		g.Expect(events[3].Entry.Metadata.Name).To(Equal(data.EntryN2I2V1.Metadata.Name))
		g.Expect(events[4].Entry.Metadata.Name).To(Equal(data.EntryN1I1V1.Metadata.Name))
	}
}

func TestKubeSource_UnparseableSegment(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, "	\n", data.YamlN2I2V1))
	g.Expect(err).To(Not(BeNil()))

	actual := removeEntryOrigins(s.Get(data.Collection1).AllSorted())
	g.Expect(actual).To(HaveLen(2))
	g.Expect(actual[0]).To(Equal(data.EntryN1I1V1))
	g.Expect(actual[1]).To(Equal(withVersion(data.EntryN2I2V1, "v2")))
}

func TestKubeSource_Unrecognized(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlUnrecognized))
	g.Expect(err).To(Not(BeNil()))

	actual := removeEntryOrigins(s.Get(data.Collection1).AllSorted())
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0]).To(Equal(data.EntryN1I1V1))
}

func TestKubeSource_UnparseableResource(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlUnparseableResource))
	g.Expect(err).To(Not(BeNil()))

	actual := removeEntryOrigins(s.Get(data.Collection1).AllSorted())
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0]).To(Equal(data.EntryN1I1V1))
}

func TestKubeSource_NonStringKey(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlNonStringKey))
	g.Expect(err).To(Not(BeNil()))

	actual := removeEntryOrigins(s.Get(data.Collection1).AllSorted())
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0]).To(Equal(data.EntryN1I1V1))
}

func TestKubeSource_Service(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSourceWithK8sMeta()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", data.GetService())
	g.Expect(err).To(BeNil())

	actual := s.Get(k8smeta.K8SCoreV1Services).AllSorted()
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0].Metadata.Name).To(Equal(resource.NewName("kube-system", "kube-dns")))
}

func TestSameNameDifferentKind(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewKubeSource(basicmeta.MustGet2().KubeSource().Resources())
	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlN1I1V1Kind2))
	g.Expect(err).To(BeNil())

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(4))
	g.Expect(events).To(ConsistOf(
		event.FullSyncFor(basicmeta.Collection1),
		event.FullSyncFor(basicmeta.Collection2),
		event.AddFor(basicmeta.Collection1, data.EntryN1I1V1),
		event.AddFor(basicmeta.Collection2, withVersion(data.EntryN1I1V1ClusterScoped, "v2"))))
}

func TestKubeSource_DefaultNamespace(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	defaultNs := "default"
	s.SetDefaultNamespace(defaultNs)

	err := s.ApplyContent("foo", data.YamlI1V1NoNamespace)
	g.Expect(err).To(BeNil())

	_, expectedName := data.EntryI1V1NoNamespace.Metadata.Name.InterpretAsNamespaceAndName()

	actual := s.Get(data.Collection1).AllSorted()
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0].Metadata.Name).To(Equal(resource.NewName(defaultNs, expectedName)))
}

func TestKubeSource_DefaultNamespaceSkipClusterScoped(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewKubeSource(basicmeta.MustGet2().KubeSource().Resources())
	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)
	s.Start()
	defer s.Stop()

	defaultNs := "default"
	s.SetDefaultNamespace(defaultNs)

	err := s.ApplyContent("foo", data.YamlI1V1NoNamespaceKind2)
	g.Expect(err).To(BeNil())

	actual := s.Get(data.Collection2).AllSorted()
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0].Metadata.Name).To(Equal(data.EntryI1V1NoNamespace.Metadata.Name))
}

func TestKubeSource_CanHandleDocumentSeparatorInComments(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	s.SetDefaultNamespace("default")

	err := s.ApplyContent("foo", data.YamlI1V1WithCommentContainingDocumentSeparator)
	g.Expect(err).To(BeNil())
	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))
}

func setupKubeSource() (*KubeSource, *fixtures.Accumulator) {
	s := NewKubeSource(basicmeta.MustGet().KubeSource().Resources())

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	return s, acc
}

func setupKubeSourceWithK8sMeta() (*KubeSource, *fixtures.Accumulator) {
	s := NewKubeSource(k8smeta.MustGet().KubeSource().Resources())

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Start()
	return s, acc
}

func withVersion(r *resource.Entry, v string) *resource.Entry {
	r = r.Clone()
	r.Metadata.Version = resource.Version(v)
	return r
}

func removeEntryOrigins(resources []*resource.Entry) []*resource.Entry {
	result := make([]*resource.Entry, len(resources))
	for i, r := range resources {
		r = r.Clone()
		r.Origin = nil
		result[i] = r
	}
	return result
}
