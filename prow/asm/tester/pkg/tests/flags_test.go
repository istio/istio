//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package tests

import (
	"github.com/google/go-cmp/cmp"
	"istio.io/istio/prow/asm/tester/pkg/asm/install/revision"
	"testing"
)

func TestRevisionsFlag(t *testing.T) {
	tcs := []struct {
		name      string
		revisions revision.Configs
		want      string
	}{
		{
			name: "no revision configs have version",
			revisions: revision.Configs{
				Configs: []revision.Config{
					{
						Name: "a",
					},
					{
						Name: "b",
					},
				}},
			want: "",
		},
		{
			name: "one revision has version, the other doesn't",
			revisions: revision.Configs{
				Configs: []revision.Config{
					{
						Name:    "a",
						Version: "1.9",
					},
					{
						Name: "b",
					},
				}},
			want: "--istio.test.revisions=a=1.9,b",
		},
		{
			name: "",
			revisions: revision.Configs{
				Configs: []revision.Config{
					{
						Name:    "a",
						Version: "1.9",
					},
					{
						Name:    "b",
						Version: "1.8",
					},
				}},
			want: "--istio.test.revisions=a=1.9,b=1.8",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := generateRevisionsFlag(&tc.revisions)
			if diff := cmp.Diff(got, tc.want); diff != "" {
				t.Errorf("got(+) different than want(-) %s", diff)
			}
		})
	}
}
