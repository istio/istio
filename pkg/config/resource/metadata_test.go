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

func TestMetadata_Clone_NilMaps(t *testing.T) {
	g := NewGomegaWithT(t)

	m := Metadata{
		FullName: NewFullName("ns1", "rs1"),
		Version:  Version("v1"),
	}

	c := m.Clone()
	g.Expect(m).To(Equal(c))
}

func TestMetadata_Clone_NonNilMaps(t *testing.T) {
	g := NewGomegaWithT(t)

	m := Metadata{
		FullName:    NewFullName("ns1", "rs1"),
		Version:     Version("v1"),
		Annotations: map[string]string{"foo": "bar"},
		Labels:      map[string]string{"l1": "l2"},
	}

	c := m.Clone()
	g.Expect(m).To(Equal(c))
}
