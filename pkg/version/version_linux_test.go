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
	"testing"

	"istio.io/istio/pkg/monitoring/monitortest"
)

func TestRecordComponentBuildTag(t *testing.T) {
	cases := []struct {
		name    string
		in      BuildInfo
		wantTag string
	}{
		{
			"record",
			BuildInfo{
				Version:       "VER",
				GitRevision:   "GITREV",
				GolangVersion: "GOLANGVER",
				BuildStatus:   "STATUS",
				GitTag:        "1.0.5-test",
			},
			"1.0.5-test",
		},
	}

	for _, v := range cases {
		t.Run(v.name, func(tt *testing.T) {
			mt := monitortest.New(t)
			v.in.RecordComponentBuildTag("test")
			mt.Assert("istio_build", map[string]string{"tag": v.wantTag}, monitortest.Exactly(1))
		})
	}
}
