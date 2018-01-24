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

import "testing"

func TestBuildInfo_String(t *testing.T) {
	cases := []struct {
		name string
		in   BuildInfo
		want string
	}{
		{"all specified", BuildInfo{
			Version:       "VER",
			GitRevision:   "GITREV",
			Host:          "HOST",
			GolangVersion: "GOLANGVER",
			DockerHub:     "DH",
			User:          "USER",
			BuildStatus:   "STATUS"}, "USER@HOST-DH-VER-GITREV-STATUS"},
		{"init", Info, "unknown@unknown-unknown-unknown-unknown-unknown"},
	}

	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			if v.in.String() != v.want {
				t.Errorf("got %s; want %s", v.in.String(), v.want)
			}
		})
	}
}
