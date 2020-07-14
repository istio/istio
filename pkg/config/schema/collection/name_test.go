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

package collection

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestNewName(t *testing.T) {
	g := NewGomegaWithT(t)

	c := NewName("c1")
	g.Expect(c.String()).To(Equal("c1"))
}

func TestNewName_Invalid(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	_ = NewName("/")
}

func TestName_String(t *testing.T) {
	g := NewGomegaWithT(t)
	c := NewName("c1")

	g.Expect(c.String()).To(Equal("c1"))
}

func TestIsValidName_Valid(t *testing.T) {
	data := []string{
		"foo",
		"9",
		"b",
		"a",
		"_",
		"a0_9",
		"a0_9/fruj_",
		"abc/def",
	}

	for _, d := range data {
		t.Run(d, func(t *testing.T) {
			g := NewGomegaWithT(t)
			b := IsValidName(d)
			g.Expect(b).To(BeTrue())
		})
	}
}

func TestIsValidName_Invalid(t *testing.T) {
	data := []string{
		"",
		"/",
		"/a",
		"a/",
		"$a/bc",
		"z//a",
	}

	for _, d := range data {
		t.Run(d, func(t *testing.T) {
			g := NewGomegaWithT(t)
			b := IsValidName(d)
			g.Expect(b).To(BeFalse())
		})
	}
}
