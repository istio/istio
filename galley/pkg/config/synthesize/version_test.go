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

package synthesize

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/resource"
)

func TestVersion(t *testing.T) {
	cases := []struct {
		prefix   string
		versions []resource.Version
		expected resource.Version
	}{
		{
			prefix: "p",
			versions: []resource.Version{
				resource.Version("v1"),
			},
			expected: resource.Version("$p_O/wmlZTvZJIo6adLqwDwQu/JHVrMb77jGjgugNQjiP4"),
		},
		{
			prefix: "pfx",
			versions: []resource.Version{
				resource.Version("v1"),
				resource.Version("v2"),
			},
			expected: resource.Version("$pfx_cMZ8rs1/SXZ1bn9ReGI47pGRRHokoNqC018mIwq2jNU"),
		},
		{
			prefix: "pfx",
			versions: []resource.Version{
				resource.Version("v2"),
				resource.Version("v1"),
			},
			expected: resource.Version("$pfx_8nucyH5RFJixr3d2YLNRB+NAKutn+DRBfXx5GmOJSRI"),
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			g := NewGomegaWithT(t)

			v := Version(c.prefix, c.versions...)
			g.Expect(v).To(Equal(c.expected))
		})
	}
}

func TestVersion_Panic(t *testing.T) {
	g := NewGomegaWithT(t)
	defer func() {
		r := recover()
		g.Expect(r).NotTo(BeNil())
	}()

	_ = Version("aa")
}
