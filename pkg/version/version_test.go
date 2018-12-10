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

import (
	"fmt"
	"runtime"
	"testing"
)

func TestNewBuildInfoFromOldString(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		expectFail bool
		want       BuildInfo
	}{
		{
			"Correct input 1",
			`Version: 1.0.0
GitRevision: 3a136c90ec5e308f236e0d7ebb5c4c5e405217f4
User: root@71a9470ea93c
Hub: docker.io/istio
GolangVersion: go1.10.1
BuildStatus: Clean
GitTag: tag
`,
			false,
			BuildInfo{Version: "1.0.0",
				GitRevision:   "3a136c90ec5e308f236e0d7ebb5c4c5e405217f4",
				User:          "root@71a9470ea93c",
				DockerHub:     "docker.io/istio",
				GolangVersion: "go1.10.1",
				BuildStatus:   "Clean",
				GitTag:        "tag"},
		},
		{
			"Invalid input 1",
			"Xuxa",
			true,
			BuildInfo{},
		},
		{
			"Invalid input 2",
			"Xuxa:Xuxo",
			true,
			BuildInfo{},
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(t *testing.T) {
			got, err := NewBuildInfoFromOldString(v.in)
			if v.expectFail && err == nil {
				t.Errorf("Expected failure, got success")
			}
			if !v.expectFail && err != nil {
				t.Errorf("Got %v, expected success", err)
			}

			if got != v.want {
				t.Errorf("Got %v, expected %v", got, v.want)
			}
		})
	}
}

func TestBuildInfo(t *testing.T) {
	versionedString := fmt.Sprintf(`version.BuildInfo{Version:"unknown", GitRevision:"unknown", User:"unknown", `+
		`Host:"unknown", GolangVersion:"%s", DockerHub:"unknown", BuildStatus:"unknown", GitTag:"unknown"}`,
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
			BuildStatus:   "STATUS",
			GitTag:        "TAG"},
			"USER@HOST-DH-VER-GITREV-STATUS",
			`version.BuildInfo{Version:"VER", GitRevision:"GITREV", User:"USER", Host:"HOST", GolangVersion:"GOLANGVER", DockerHub:"DH", ` +
				`BuildStatus:"STATUS", GitTag:"TAG"}`},

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
