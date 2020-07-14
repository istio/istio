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

package resource

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestStringMap_Clone_Nil(t *testing.T) {
	g := NewGomegaWithT(t)

	var s StringMap

	c := s.Clone()
	g.Expect(c).To(BeNil())
}

func TestStringMap_Clone_NonNil(t *testing.T) {
	g := NewGomegaWithT(t)

	var s StringMap = map[string]string{
		"foo": "bar",
	}

	c := s.Clone()
	g.Expect(c).NotTo(BeNil())
	g.Expect(c).To(HaveLen(1))
	g.Expect(c["foo"]).To(Equal("bar"))
}

func TestStringMap_CloneOrCreate_Nil(t *testing.T) {
	g := NewGomegaWithT(t)

	var s StringMap

	c := s.CloneOrCreate()
	g.Expect(c).NotTo(BeNil())
	g.Expect(c).To(HaveLen(0))
}

func TestStringMap_CloneOrCreate_NonNil(t *testing.T) {
	g := NewGomegaWithT(t)

	var s StringMap = map[string]string{
		"foo": "bar",
	}

	c := s.CloneOrCreate()
	g.Expect(c).NotTo(BeNil())
	g.Expect(c).To(HaveLen(1))
	g.Expect(c["foo"]).To(Equal("bar"))
}

func TestStringMap_Delete_NonNil(t *testing.T) {
	g := NewGomegaWithT(t)

	var s StringMap = map[string]string{
		"foo": "bar",
	}

	s.Delete("foo")
	g.Expect(s).NotTo(BeNil())
	g.Expect(s).To(HaveLen(0))
}

func TestStringMap_Delete_Nil(t *testing.T) {
	g := NewGomegaWithT(t)

	var s StringMap

	s.Delete("foo")
	g.Expect(s).To(BeNil())
}
