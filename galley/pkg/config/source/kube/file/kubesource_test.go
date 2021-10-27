/*
 Copyright Istio Authors

 Licensed under the Apache License, Version 2.0 (the "License");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package file

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	yamlv3 "gopkg.in/yaml.v3"

	kubeyaml2 "istio.io/istio/pilot/pkg/config/file/util/kubeyaml"
	"istio.io/istio/pkg/config/event"
	basicmeta2 "istio.io/istio/pkg/config/legacy/testing/basicmeta"
	data2 "istio.io/istio/pkg/config/legacy/testing/data"
	fixtures2 "istio.io/istio/pkg/config/legacy/testing/fixtures"
	k8smeta2 "istio.io/istio/pkg/config/legacy/testing/k8smeta"
	"istio.io/istio/pkg/config/resource"
)

func TestKubeSource_ApplyContent(t *testing.T) {
	g := NewWithT(t)

	s, acc := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", data2.YamlN1I1V1)
	g.Expect(err).To(BeNil())

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))

	actual := s.Get(basicmeta2.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))

	g.Expect(actual[0].Metadata.FullName).To(Equal(data2.EntryN1I1V1.Metadata.FullName))

	g.Expect(acc.Events()).To(HaveLen(2))
	g.Expect(acc.Events()[0].Kind).To(Equal(event.FullSync))
	g.Expect(acc.Events()[1].Kind).To(Equal(event.Added))
	g.Expect(acc.Events()[1].Resource.Metadata.FullName).To(Equal(data2.EntryN1I1V1.Metadata.FullName))
}

func TestKubeSource_ApplyContent_BeforeStart(t *testing.T) {
	g := NewWithT(t)

	s, acc := setupKubeSource()
	defer s.Stop()

	err := s.ApplyContent("foo", data2.YamlN1I1V1)
	g.Expect(err).To(BeNil())
	s.Start()

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))

	actual := s.Get(basicmeta2.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))

	g.Expect(actual[0].Metadata.FullName).To(Equal(data2.EntryN1I1V1.Metadata.FullName))

	g.Expect(acc.Events()).To(HaveLen(2))
	g.Expect(acc.Events()[0].Kind).To(Equal(event.Added))
	g.Expect(acc.Events()[0].Resource.Metadata.FullName).To(Equal(data2.EntryN1I1V1.Metadata.FullName))
	g.Expect(acc.Events()[1].Kind).To(Equal(event.FullSync))
}

func TestKubeSource_ApplyContent_Unchanged0Add1(t *testing.T) {
	g := NewWithT(t)

	s, acc := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml2.JoinString(data2.YamlN1I1V1, data2.YamlN2I2V1))
	g.Expect(err).To(BeNil())

	actual := s.Get(basicmeta2.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(2))
	g.Expect(actual[0].Metadata.FullName).To(Equal(data2.EntryN1I1V1.Metadata.FullName))
	g.Expect(actual[1].Metadata.FullName).To(Equal(data2.EntryN2I2V1.Metadata.FullName))

	err = s.ApplyContent("foo", kubeyaml2.JoinString(data2.YamlN2I2V2, data2.YamlN3I3V1))
	g.Expect(err).To(BeNil())

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))

	actual = s.Get(basicmeta2.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(2))
	g.Expect(actual[0].Metadata.FullName).To(Equal(data2.EntryN2I2V2.Metadata.FullName))
	g.Expect(actual[1].Metadata.FullName).To(Equal(data2.EntryN3I3V1.Metadata.FullName))

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(6))
	g.Expect(events[0].Kind).To(Equal(event.FullSync))
	g.Expect(events[1].Kind).To(Equal(event.Added))
	fixtures2.ExpectEqual(t, events[1].Resource, data2.EntryN1I1V1)
	g.Expect(events[2].Kind).To(Equal(event.Added))
	fixtures2.ExpectEqual(t, events[2].Resource, withVersion(data2.EntryN2I2V1, "v2"))
	g.Expect(events[3].Kind).To(Equal(event.Updated))
	fixtures2.ExpectEqual(t, events[3].Resource, withVersion(data2.EntryN2I2V2, "v3"))
	g.Expect(events[4].Kind).To(Equal(event.Added))
	fixtures2.ExpectEqual(t, events[4].Resource, withVersion(data2.EntryN3I3V1, "v4"))
	g.Expect(events[5].Kind).To(Equal(event.Deleted))
	g.Expect(events[5].Resource.Metadata.FullName).To(Equal(data2.EntryN1I1V1.Metadata.FullName))
}

func TestKubeSource_RemoveContent(t *testing.T) {
	g := NewWithT(t)

	s, acc := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml2.JoinString(data2.YamlN1I1V1, data2.YamlN2I2V1))
	g.Expect(err).To(BeNil())
	err = s.ApplyContent("bar", kubeyaml2.JoinString(data2.YamlN3I3V1))
	g.Expect(err).To(BeNil())

	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"bar": {}, "foo": {}}))

	s.RemoveContent("foo")
	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"bar": {}}))

	actual := s.Get(basicmeta2.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(6))
	fixtures2.ExpectContainEvents(t, events[0:4],
		event.FullSyncFor(basicmeta2.K8SCollection1),
		event.AddFor(basicmeta2.K8SCollection1, data2.EntryN1I1V1),
		event.AddFor(basicmeta2.K8SCollection1, withVersion(data2.EntryN2I2V1, "v2")),
		event.AddFor(basicmeta2.K8SCollection1, withVersion(data2.EntryN3I3V1, "v3")))

	//  Delete events can appear out of order.
	g.Expect(events[4].Kind).To(Equal(event.Deleted))
	g.Expect(events[5].Kind).To(Equal(event.Deleted))

	if events[4].Resource.Metadata.FullName == data2.EntryN1I1V1.Metadata.FullName {
		fixtures2.ExpectContainEvents(t, events[4:],
			event.DeleteForResource(basicmeta2.K8SCollection1, data2.EntryN1I1V1),
			event.DeleteForResource(basicmeta2.K8SCollection1, withVersion(data2.EntryN2I2V1, "v2")))
	} else {
		fixtures2.ExpectContainEvents(t, events[4:],
			event.DeleteForResource(basicmeta2.K8SCollection1, withVersion(data2.EntryN2I2V1, "v2")),
			event.DeleteForResource(basicmeta2.K8SCollection1, data2.EntryN1I1V1))
	}
}

func TestKubeSource_Clear(t *testing.T) {
	g := NewWithT(t)

	s, acc := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml2.JoinString(data2.YamlN1I1V1, data2.YamlN2I2V1))
	g.Expect(err).To(BeNil())

	s.Clear()

	actual := s.Get(basicmeta2.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(0))

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(5))
	g.Expect(events[0].Kind).To(Equal(event.FullSync))
	g.Expect(events[1].Kind).To(Equal(event.Added))
	fixtures2.ExpectEqual(t, events[1].Resource, data2.EntryN1I1V1)
	g.Expect(events[2].Kind).To(Equal(event.Added))
	fixtures2.ExpectEqual(t, events[2].Resource, withVersion(data2.EntryN2I2V1, "v2"))

	g.Expect(events[3].Kind).To(Equal(event.Deleted))
	g.Expect(events[4].Kind).To(Equal(event.Deleted))

	if events[3].Resource.Metadata.FullName == data2.EntryN1I1V1.Metadata.FullName {
		g.Expect(events[3].Resource.Metadata.FullName).To(Equal(data2.EntryN1I1V1.Metadata.FullName))
		g.Expect(events[4].Resource.Metadata.FullName).To(Equal(data2.EntryN2I2V1.Metadata.FullName))
	} else {
		g.Expect(events[3].Resource.Metadata.FullName).To(Equal(data2.EntryN2I2V1.Metadata.FullName))
		g.Expect(events[4].Resource.Metadata.FullName).To(Equal(data2.EntryN1I1V1.Metadata.FullName))
	}
}

func TestKubeSource_UnparseableSegment(t *testing.T) {
	g := NewWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml2.JoinString(data2.YamlN1I1V1, "invalidyaml\n", data2.YamlN2I2V1))
	g.Expect(err).To(Not(BeNil()))

	actual := removeEntryOrigins(s.Get(basicmeta2.K8SCollection1.Name()).AllSorted())
	g.Expect(actual).To(HaveLen(2))
	fixtures2.ExpectEqual(t, actual[0], data2.EntryN1I1V1)
	fixtures2.ExpectEqual(t, actual[1], withVersion(data2.EntryN2I2V1, "v2"))
}

func TestKubeSource_Unrecognized(t *testing.T) {
	g := NewWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml2.JoinString(data2.YamlN1I1V1, data2.YamlUnrecognized))
	g.Expect(err).To(BeNil())

	// Even though we got no error, we still only parsed one resource as the unrecognized one was ignored.
	actual := removeEntryOrigins(s.Get(basicmeta2.K8SCollection1.Name()).AllSorted())
	g.Expect(actual).To(HaveLen(1))
	fixtures2.ExpectEqual(t, actual[0], data2.EntryN1I1V1)
}

func TestKubeSource_UnparseableResource(t *testing.T) {
	g := NewWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml2.JoinString(data2.YamlN1I1V1, data2.YamlUnparseableResource))
	g.Expect(err).To(Not(BeNil()))

	actual := removeEntryOrigins(s.Get(basicmeta2.K8SCollection1.Name()).AllSorted())
	g.Expect(actual).To(HaveLen(1))
	fixtures2.ExpectEqual(t, actual[0], data2.EntryN1I1V1)
}

func TestKubeSource_NonStringKey(t *testing.T) {
	g := NewWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml2.JoinString(data2.YamlN1I1V1, data2.YamlNonStringKey))
	g.Expect(err).To(Not(BeNil()))

	actual := removeEntryOrigins(s.Get(basicmeta2.K8SCollection1.Name()).AllSorted())
	g.Expect(actual).To(HaveLen(1))
	fixtures2.ExpectEqual(t, actual[0], data2.EntryN1I1V1)
}

func TestKubeSource_Service(t *testing.T) {
	g := NewWithT(t)

	s, _ := setupKubeSourceWithK8sMeta()
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", data2.GetService())
	g.Expect(err).To(BeNil())

	actual := s.Get(k8smeta2.K8SCoreV1Services.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0].Metadata.FullName).To(Equal(resource.NewFullName("kube-system", "kube-dns")))
}

func TestSameNameDifferentKind(t *testing.T) {
	g := NewWithT(t)

	meta := basicmeta2.MustGet2().KubeCollections()
	col1 := meta.MustFind(basicmeta2.K8SCollection1.Name().String())

	s := NewKubeSource(meta)
	acc := &fixtures2.Accumulator{}
	s.Dispatch(acc)
	s.Start()
	defer s.Stop()

	err := s.ApplyContent("foo", kubeyaml2.JoinString(data2.YamlN1I1V1, data2.YamlN1I1V1Kind2))
	g.Expect(err).To(BeNil())

	events := acc.EventsWithoutOrigins()
	g.Expect(events).To(HaveLen(4))
	fixtures2.ExpectContainEvents(t, events,
		event.FullSyncFor(col1),
		event.FullSyncFor(data2.K8SCollection2),
		event.AddFor(col1, data2.EntryN1I1V1),
		event.AddFor(data2.K8SCollection2, withVersion(data2.EntryN1I1V1ClusterScoped, "v2")))
}

func TestKubeSource_DefaultNamespace(t *testing.T) {
	g := NewWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	defaultNs := resource.Namespace("default")
	s.SetDefaultNamespace(defaultNs)

	err := s.ApplyContent("foo", data2.YamlI1V1NoNamespace)
	g.Expect(err).To(BeNil())

	expectedName := data2.EntryI1V1NoNamespace.Metadata.FullName.Name

	actual := s.Get(basicmeta2.K8SCollection1.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0].Metadata.FullName).To(Equal(resource.NewFullName(defaultNs, expectedName)))
}

func TestKubeSource_DefaultNamespaceSkipClusterScoped(t *testing.T) {
	g := NewWithT(t)

	s := NewKubeSource(basicmeta2.MustGet2().KubeCollections())
	acc := &fixtures2.Accumulator{}
	s.Dispatch(acc)
	s.Start()
	defer s.Stop()

	defaultNs := resource.Namespace("default")
	s.SetDefaultNamespace(defaultNs)

	err := s.ApplyContent("foo", data2.YamlI1V1NoNamespaceKind2)
	g.Expect(err).To(BeNil())

	actual := s.Get(data2.K8SCollection2.Name()).AllSorted()
	g.Expect(actual).To(HaveLen(1))
	g.Expect(actual[0].Metadata.FullName).To(Equal(data2.EntryI1V1NoNamespace.Metadata.FullName))
}

func TestKubeSource_CanHandleDocumentSeparatorInComments(t *testing.T) {
	g := NewWithT(t)

	s, _ := setupKubeSource()
	s.Start()
	defer s.Stop()

	s.SetDefaultNamespace("default")

	err := s.ApplyContent("foo", data2.YamlI1V1WithCommentContainingDocumentSeparator)
	g.Expect(err).To(BeNil())
	g.Expect(s.ContentNames()).To(Equal(map[string]struct{}{"foo": {}}))
}

func setupKubeSource() (*KubeSource, *fixtures2.Accumulator) {
	s := NewKubeSource(basicmeta2.MustGet().KubeCollections())

	acc := &fixtures2.Accumulator{}
	s.Dispatch(acc)

	return s, acc
}

func setupKubeSourceWithK8sMeta() (*KubeSource, *fixtures2.Accumulator) {
	s := NewKubeSource(k8smeta2.MustGet().KubeCollections())

	acc := &fixtures2.Accumulator{}
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

func TestBuildFieldPathMap(t *testing.T) {
	yamlResource := map[string]interface{}{
		"key":    "value",
		"array":  []string{"a", "b", "c", "d", "e"},
		"number": 1,
		"sliceMap": []map[string]string{
			{"a": "1"}, {"b": "2"},
		},
	}

	g := NewWithT(t)

	yamlMarshal, err := yamlv3.Marshal(&yamlResource)
	g.Expect(err).To(BeNil())

	result := make(map[string]int)

	yamlNode := yamlv3.Node{}

	err = yamlv3.Unmarshal(yamlMarshal, &yamlNode)
	g.Expect(err).To(BeNil())

	BuildFieldPathMap(yamlNode.Content[0], 1, "", result)

	g.Expect(fmt.Sprintf("%v", result)).To(Equal("map[{.array[0]}:2 {.array[1]}:3 {.array[2]}:4 " +
		"{.array[3]}:5 {.array[4]}:6 {.key}:7 {.number}:8 {.sliceMap[0].a}:10 {.sliceMap[1].b}:11]"))
}
