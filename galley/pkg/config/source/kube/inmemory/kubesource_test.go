// Copyright Istio Authors
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

	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/galley/pkg/config/testing/data"
	"istio.io/istio/galley/pkg/config/testing/fixtures"
	"istio.io/istio/galley/pkg/config/testing/k8smeta"
	"istio.io/istio/galley/pkg/config/util/kubeyaml"
	"istio.io/istio/pkg/config/event"
	"istio.io/istio/pkg/config/resource"
)

func TestKubeSource_ApplyContent(t *testing.T) {
	g := NewGomegaWithT(t)

	s, acc := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", data.YamlN1I1V1)
	g.Expect(err).To(BeNil())

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))

	actual := s.Get(basicmeta.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))

	g.Expect(actual[0].Metadata.FullName).To(Equal(data.EntryN1I1V1.Metadata.FullName))

	g.Expect(acc.Events()).To(HaveLen(2))
	g.Expect(acc.Events()[0].Kind).To(Equal(event.FullSync))
	g.Expect(acc.Events()[1].Kind).To(Equal(event.Added))
	g.Expect(acc.Events()[1].Resource.Metadata.FullName).To(Equal(data.EntryN1I1V1.Metadata.FullName))
}

func TestKubeSource_ApplyContent_BeforeStart(t *testing.T) {
	g := NewGomegaWithT(t)

	s, acc := setupKubeSource()
	defer s.Stop()

	err := s.ApplyContent("foo", data.YamlN1I1V1)
	g.Expect(err).To(BeNil())
	s.Start()

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))

	actual := s.Get(basicmeta.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))

	g.Expect(actual[0].Metadata.FullName).To(Equal(data.EntryN1I1V1.Metadata.FullName))

	g.Expect(acc.Events()).To(HaveLen(2))
	g.Expect(acc.Events()[0].Kind).To(Equal(event.Added))
	g.Expect(acc.Events()[0].Resource.Metadata.FullName).To(Equal(data.EntryN1I1V1.Metadata.FullName))
	g.Expect(acc.Events()[1].Kind).To(Equal(event.FullSync))
}

func TestKubeSource_ApplyContent_Unchanged0Add1(t *testing.T) {
	g := NewGomegaWithT(t)

	s, acc := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlN2I2V1))
	g.Expect(err).To(BeNil())

	actual := s.Get(basicmeta.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(2))
	g.Expect(actual[0].Metadata.FullName).To(Equal(data.EntryN1I1V1.Metadata.FullName))
	g.Expect(actual[1].Metadata.FullName).To(Equal(data.EntryN2I2V1.Metadata.FullName))

	err = s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN2I2V2, data.YamlN3I3V1))
	g.Expect(err).To(BeNil())

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))

	actual = s.Get(basicmeta.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(2))
	g.Expect(actual[0].Metadata.FullName).To(Equal(data.EntryN2I2V2.Metadata.FullName))
	g.Expect(actual[1].Metadata.FullName).To(Equal(data.EntryN3I3V1.Metadata.FullName))

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(6))
	g.Expect(events[0].Kind).To(Equal(event.FullSync))
	g.Expect(events[1].Kind).To(Equal(event.Added))
	fixtures.ExpectEqual(t, events[1].Resource, data.EntryN1I1V1)
	g.Expect(events[2].Kind).To(Equal(event.Added))
	fixtures.ExpectEqual(t, events[2].Resource, withVersion(data.EntryN2I2V1, "v2"))
	g.Expect(events[3].Kind).To(Equal(event.Updated))
	fixtures.ExpectEqual(t, events[3].Resource, withVersion(data.EntryN2I2V2, "v3"))
	g.Expect(events[4].Kind).To(Equal(event.Added))
	fixtures.ExpectEqual(t, events[4].Resource, withVersion(data.EntryN3I3V1, "v4"))
	g.Expect(events[5].Kind).To(Equal(event.Deleted))
	g.Expect(events[5].Resource.Metadata.FullName).To(Equal(data.EntryN1I1V1.Metadata.FullName))
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

	actual := s.Get(basicmeta.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(6))
	fixtures.ExpectContainEvents(t, events[0:4],
		event.FullSyncFor(basicmeta.K8SCollection1),
		event.AddFor(basicmeta.K8SCollection1, data.EntryN1I1V1),
		event.AddFor(basicmeta.K8SCollection1, withVersion(data.EntryN2I2V1, "v2")),
		event.AddFor(basicmeta.K8SCollection1, withVersion(data.EntryN3I3V1, "v3")))

	//  Delete events can appear out of order.
	g.Expect(events[4].Kind).To(Equal(event.Deleted))
	g.Expect(events[5].Kind).To(Equal(event.Deleted))

	if events[4].Resource.Metadata.FullName == data.EntryN1I1V1.Metadata.FullName {
		fixtures.ExpectContainEvents(t, events[4:],
			event.DeleteForResource(basicmeta.K8SCollection1, data.EntryN1I1V1),
			event.DeleteForResource(basicmeta.K8SCollection1, withVersion(data.EntryN2I2V1, "v2")))
	} else {
		fixtures.ExpectContainEvents(t, events[4:],
			event.DeleteForResource(basicmeta.K8SCollection1, withVersion(data.EntryN2I2V1, "v2")),
			event.DeleteForResource(basicmeta.K8SCollection1, data.EntryN1I1V1))
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

	actual := s.Get(basicmeta.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(0))

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(5))
	g.Expect(events[0].Kind).To(Equal(event.FullSync))
	g.Expect(events[1].Kind).To(Equal(event.Added))
	fixtures.ExpectEqual(t, events[1].Resource, data.EntryN1I1V1)
	g.Expect(events[2].Kind).To(Equal(event.Added))
	fixtures.ExpectEqual(t, events[2].Resource, withVersion(data.EntryN2I2V1, "v2"))

	g.Expect(events[3].Kind).To(Equal(event.Deleted))
	g.Expect(events[4].Kind).To(Equal(event.Deleted))

	if events[3].Resource.Metadata.FullName == data.EntryN1I1V1.Metadata.FullName {
		g.Expect(events[3].Resource.Metadata.FullName).To(Equal(data.EntryN1I1V1.Metadata.FullName))
		g.Expect(events[4].Resource.Metadata.FullName).To(Equal(data.EntryN2I2V1.Metadata.FullName))
	} else {
		g.Expect(events[3].Resource.Metadata.FullName).To(Equal(data.EntryN2I2V1.Metadata.FullName))
		g.Expect(events[4].Resource.Metadata.FullName).To(Equal(data.EntryN1I1V1.Metadata.FullName))
	}
}

func TestKubeSource_UnparseableSegment(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, "invalidyaml\n", data.YamlN2I2V1))
	g.Expect(err).To(Not(BeNil()))

	actual := removeEntryOrigins(s.Get(basicmeta.K8SCollection1.Name()).AllSorted())
	g.Expect(actual).To(HaveLen(2))
	fixtures.ExpectEqual(t, actual[0], data.EntryN1I1V1)
	fixtures.ExpectEqual(t, actual[1], withVersion(data.EntryN2I2V1, "v2"))
}

func TestKubeSource_Unrecognized(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlUnrecognized))
	g.Expect(err).To(BeNil())

	// Even though we got no error, we still only parsed one resource as the unrecognized one was ignored.
	actual := removeEntryOrigins(s.Get(basicmeta.K8SCollection1.Name()).AllSorted())
	g.Expect(actual).To(HaveLen(1))
	fixtures.ExpectEqual(t, actual[0], data.EntryN1I1V1)
}

func TestKubeSource_UnparseableResource(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlUnparseableResource))
	g.Expect(err).To(Not(BeNil()))

	actual := removeEntryOrigins(s.Get(basicmeta.K8SCollection1.Name()).AllSorted())
	g.Expect(actual).To(HaveLen(1))
	fixtures.ExpectEqual(t, actual[0], data.EntryN1I1V1)
}

func TestKubeSource_NonStringKey(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlNonStringKey))
	g.Expect(err).To(Not(BeNil()))

	actual := removeEntryOrigins(s.Get(basicmeta.K8SCollection1.Name()).AllSorted())
	g.Expect(actual).To(HaveLen(1))
	fixtures.ExpectEqual(t, actual[0], data.EntryN1I1V1)
}

func TestKubeSource_Service(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSourceWithK8sMeta()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", data.GetService())
	g.Expect(err).To(BeNil())

	actual := s.Get(k8smeta.K8SCoreV1Services.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0].Metadata.FullName).To(Equal(resource.NewFullName("kube-system", "kube-dns")))
}

func TestSameNameDifferentKind(t *testing.T) {
	g := NewGomegaWithT(t)

	meta := basicmeta.MustGet2().KubeCollections()
	col1 := meta.MustFind(basicmeta.K8SCollection1.Name().String())

	s := NewKubeSource(meta)
	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml.JoinString(data.YamlN1I1V1, data.YamlN1I1V1Kind2))
	g.Expect(err).To(BeNil())

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(4))
	fixtures.ExpectContainEvents(t, events,
		event.FullSyncFor(col1),
		event.FullSyncFor(data.K8SCollection2),
		event.AddFor(col1, data.EntryN1I1V1),
		event.AddFor(data.K8SCollection2, withVersion(data.EntryN1I1V1ClusterScoped, "v2")))
}

func TestKubeSource_DefaultNamespace(t *testing.T) {
	g := NewGomegaWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	defaultNs := resource.Namespace("default")
	s.SetDefaultNamespace(defaultNs)

	err := s.ApplyContent("foo", data.YamlI1V1NoNamespace)
	g.Expect(err).To(BeNil())

	expectedName := data.EntryI1V1NoNamespace.Metadata.FullName.Name

	actual := s.Get(basicmeta.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0].Metadata.FullName).To(Equal(resource.NewFullName(defaultNs, expectedName)))
}

func TestKubeSource_DefaultNamespaceSkipClusterScoped(t *testing.T) {
	g := NewGomegaWithT(t)

	s := NewKubeSource(basicmeta.MustGet2().KubeCollections())
	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)
	s.Start()
	defer s.Stop()

	defaultNs := resource.Namespace("default")
	s.SetDefaultNamespace(defaultNs)

	err := s.ApplyContent("foo", data.YamlI1V1NoNamespaceKind2)
	g.Expect(err).To(BeNil())

	actual := s.Get(data.K8SCollection2.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0].Metadata.FullName).To(Equal(data.EntryI1V1NoNamespace.Metadata.FullName))
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
	s := NewKubeSource(basicmeta.MustGet().KubeCollections())

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	return s, acc
}

func setupKubeSourceWithK8sMeta() (*KubeSource, *fixtures.Accumulator) {
	s := NewKubeSource(k8smeta.MustGet().KubeCollections())

	acc := &fixtures.Accumulator{}
	s.Dispatch(acc)

	s.Start()
	return s, acc
}

func withVersion(r *resource.Instance, v string) *resource.Instance {
	r = r.Clone()
	r.Metadata.Version = resource.Version(v)
	return r
}

func removeEntryOrigins(resources []*resource.Instance) []*resource.Instance {
	result := make([]*resource.Instance, len(resources))
	for i, r := range resources {
		r = r.Clone()
		r.Origin = nil
		result[i] = r
	}
	return result
}
