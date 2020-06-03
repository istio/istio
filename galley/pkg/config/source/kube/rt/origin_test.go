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

package rt

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
)

func TestPositionString(t *testing.T) {
	testcases := []struct {
		filename string
		line     int
		output   string
	}{
		{
			filename: "test.yaml",
			line:     1,
			output:   "test.yaml:1",
		},
		{
			filename: "test.yaml",
			line:     0,
			output:   "test.yaml",
		},
		{
			filename: "test.json",
			line:     1,
			output:   "test.json",
		},
		{
			filename: "-",
			line:     1,
			output:   "-:1",
		},
		{
			filename: "",
			line:     1,
			output:   "",
		},
	}
	for i, tc := range testcases {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			g := NewGomegaWithT(t)

			p := Position{Filename: tc.filename, Line: tc.line}
			g.Expect(p.String()).To(Equal(tc.output))
		})
	}
}
