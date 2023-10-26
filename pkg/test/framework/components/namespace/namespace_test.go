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

package namespace

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestConfigRevisionOverwrite(t *testing.T) {
	testCases := []struct {
		name string

		// test inputs.
		revision string
		cfg      Config

		// expected results.
		wantRevision string
	}{
		{
			name:     "NoOverwriteEmptyInput",
			revision: "",
			cfg: Config{
				Prefix:   "default",
				Revision: "istio.io/rev=XXX",
			},
			wantRevision: "istio.io/rev=XXX",
		},
		{
			name:     "NoOverwriteNonEmptyInput",
			revision: "BadRevision",
			cfg: Config{
				Prefix:   "default",
				Revision: "istio.io/rev=XXX",
			},
			wantRevision: "istio.io/rev=XXX",
		},
		{
			name:     "OverwriteNonEmptyInput",
			revision: "istio.io/rev=canary",
			cfg: Config{
				Prefix:   "default",
				Revision: "",
			},
			wantRevision: "istio.io/rev=canary",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.cfg.overwriteRevisionIfEmpty(tc.revision)
			g := NewWithT(t)
			g.Expect(tc.cfg.Revision).Should(Equal(tc.wantRevision))
		})
	}
}
