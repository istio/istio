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

package strategy

import (
	"reflect"
	"testing"

	. "github.com/onsi/gomega"
)

func TestCreate_Immediate(t *testing.T) {
	g := NewGomegaWithT(t)

	s, err := Create(immediate)
	g.Expect(err).To(BeNil())

	g.Expect(reflect.TypeOf(s)).To(Equal(reflect.TypeOf(&Immediate{})))
}

func TestCreate_Debounce(t *testing.T) {
	g := NewGomegaWithT(t)

	s, err := Create(debounce)
	g.Expect(err).To(BeNil())

	g.Expect(reflect.TypeOf(s)).To(Equal(reflect.TypeOf(&Debounce{})))
}

func TestCreate_Unknown(t *testing.T) {
	g := NewGomegaWithT(t)

	_, err := Create("foo")
	g.Expect(err).NotTo(BeNil())
}
