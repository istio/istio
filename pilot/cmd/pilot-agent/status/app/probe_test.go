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

package app_test

import (
	"errors"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pilot/cmd/pilot-agent/status/app"
)

func TestReadinessProbeURLPath(t *testing.T) {
	g := NewGomegaWithT(t)
	expected := "/app-health/mycontainer/readyz"
	actual := string(app.ReadinessProbeURLPath("mycontainer"))
	g.Expect(actual).To(Equal(expected))
}

func TestLivenessProbeURLPath(t *testing.T) {
	g := NewGomegaWithT(t)
	expected := "/app-health/mycontainer/livez"
	actual := string(app.LivenessProbeURLPath("mycontainer"))
	g.Expect(actual).To(Equal(expected))
}

func TestParseProbeMapJSON(t *testing.T) {
	testCases := []struct {
		name      string
		httpProbe string
		err       error
	}{
		// Json can't be parsed.
		{
			name:      "InvalidProberEncoding",
			httpProbe: "invalid-prober-json-encoding",
			err:       errors.New("failed to decode"),
		},
		// map key is not well formed.
		{
			name:      "MapKeyNotWellFormed",
			httpProbe: `{"abc": {"path": "/app-foo/health"}}`,
			err:       errors.New("invalid key"),
		},
		// Port is not Int typed.
		{
			name:      "PortNotInt",
			httpProbe: `{"/app-health/hello-world/readyz": {"path": "/hello/sunnyvale", "port": "container-port-dontknow"}}`,
			err:       errors.New("must be int type"),
		},
		// A valid input.
		{
			name: "ValidInput",
			httpProbe: `{"/app-health/hello-world/readyz": {"path": "/hello/sunnyvale", "port": 8080},` +
				`"/app-health/business/livez": {"path": "/buisiness/live", "port": 9090}}`,
		},
		// A valid input with empty probing path, which happens when HTTPGetAction.Path is not specified.
		{
			name: "EmptyProbingPath",
			httpProbe: `{"/app-health/hello-world/readyz": {"path": "/hello/sunnyvale", "port": 8080},` +
				`"/app-health/business/livez": {"port": 9090}}`,
		},
		// A valid input without any prober info.
		{
			name:      "NoProberInfo",
			httpProbe: `{}`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)
			_, err := app.ParseProbeMapJSON(tc.httpProbe)
			if tc.err == nil {
				g.Expect(err).To(BeNil())
			} else {
				g.Expect(err).ToNot(BeNil())

				// error case, error string should match.
				if !strings.Contains(err.Error(), tc.err.Error()) {
					t.Errorf("test case failed [%v], expect error %v, got %v", tc.httpProbe, tc.err, err)
				}
			}
		})
	}
}
