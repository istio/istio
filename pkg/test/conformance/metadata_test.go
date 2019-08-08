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

package conformance

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	. "github.com/onsi/gomega"
)

func TestMetadata(t *testing.T) {
	cases := []struct {
		Input    string
		Prefix   string
		Expected *Metadata
	}{
		{
			Input:    ``,
			Expected: &Metadata{},
		},
		{
			Input: `
skip: true
exclusive: true
labels:
  - a
  - b
  - c
environments:
  - kube
  - native
`,
			Prefix: "foo",
			Expected: &Metadata{
				Name:         "foo",
				Skip:         true,
				Exclusive:    true,
				Labels:       []string{"a", "b", "c"},
				Environments: []string{"kube", "native"},
			},
		},
	}

	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			g := NewGomegaWithT(t)

			d, err := ioutil.TempDir(os.TempDir(), "TestMetadata")
			g.Expect(err).To(BeNil())

			err = ioutil.WriteFile(path.Join(d, MetadataFileName), []byte(c.Input), os.ModePerm)
			g.Expect(err).To(BeNil())

			m, err := loadMetadata(d, c.Prefix)
			g.Expect(err).To(BeNil())

			g.Expect(m).To(Equal(c.Expected))
		})
	}
}
