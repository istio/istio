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

package collection_test

import (
	"testing"

	. "github.com/onsi/gomega"

	coll "istio.io/istio/galley/pkg/config/collection"
	"istio.io/istio/galley/pkg/config/testing/basicmeta"
	"istio.io/istio/pkg/config/schema/collection"
)

func TestNewSet(t *testing.T) {
	g := NewGomegaWithT(t)

	s := coll.NewSet(collection.NewSchemasBuilder().MustAdd(basicmeta.K8SCollection1).MustAdd(basicmeta.Collection2).Build())

	s1 := s.Collection(basicmeta.K8SCollection1.Name())
	g.Expect(s1).NotTo(BeNil())
	s2 := s.Collection(basicmeta.Collection2.Name())
	g.Expect(s2).NotTo(BeNil())

	s3 := s.Collection(collection.NewName("foobar"))
	g.Expect(s3).To(BeNil())
}

func TestNewSetFromCollections(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := coll.New(basicmeta.K8SCollection1)
	g.Expect(s1).NotTo(BeNil())
	s2 := coll.New(basicmeta.Collection2)
	g.Expect(s2).NotTo(BeNil())

	s := coll.NewSetFromCollections([]*coll.Instance{s1, s2})

	c := s.Collection(basicmeta.K8SCollection1.Name())
	g.Expect(c).NotTo(BeNil())
	c = s.Collection(basicmeta.Collection2.Name())
	g.Expect(c).NotTo(BeNil())

	c = s.Collection(collection.NewName("foobar"))
	g.Expect(c).To(BeNil())
}

func TestSet_Clone(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := coll.New(basicmeta.K8SCollection1)
	g.Expect(s1).NotTo(BeNil())
	s2 := coll.New(basicmeta.Collection2)
	g.Expect(s2).NotTo(BeNil())

	s := coll.NewSetFromCollections([]*coll.Instance{s1, s2})

	s = s.Clone()

	c := s.Collection(basicmeta.K8SCollection1.Name())
	g.Expect(c).NotTo(BeNil())
	c = s.Collection(basicmeta.Collection2.Name())
	g.Expect(c).NotTo(BeNil())

	c = s.Collection(collection.NewName("foobar"))
	g.Expect(c).To(BeNil())
}

func TestSet_Names(t *testing.T) {
	g := NewGomegaWithT(t)

	s1 := coll.New(basicmeta.K8SCollection1)
	s2 := coll.New(basicmeta.Collection2)

	s := coll.NewSetFromCollections([]*coll.Instance{s1, s2})
	names := s.Names()
	g.Expect(names).To(ConsistOf(
		basicmeta.K8SCollection1.Name(),
		basicmeta.Collection2.Name()))
}
