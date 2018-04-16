// Copyright 2017 Istio Authors
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

package version

import "fmt"
import "runtime"
import "testing"

func TestBuildInfo(t *testing.T) {
	versionedString := fmt.Sprintf(`Version: unknown
GitRevision: unknown
User: unknown@unknown
Hub: unknown
GolangVersion: %v
BuildStatus: unknown
`,
		runtime.Version())

	cases := []struct {
		name     string
		in       BuildInfo
		want     string
		longWant string
	}{
		{"all specified", BuildInfo{
			Version:       "VER",
			GitRevision:   "GITREV",
			Host:          "HOST",
			GolangVersion: "GOLANGVER",
			DockerHub:     "DH",
			User:          "USER",
			BuildStatus:   "STATUS"},
			"USER@HOST-DH-VER-GITREV-STATUS", `Version: VER
GitRevision: GITREV
User: USER@HOST
Hub: DH
GolangVersion: GOLANGVER
BuildStatus: STATUS
`},

		{"init", Info, "unknown@unknown-unknown-unknown-unknown-unknown", versionedString}}

	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			if v.in.String() != v.want {
				t.Errorf("got %s; want %s", v.in.String(), v.want)
			}

			if v.in.LongForm() != v.longWant {
				t.Errorf("got\n%s\nwant\n%s", v.in.LongForm(), v.longWant)
			}
		})
	}
}
