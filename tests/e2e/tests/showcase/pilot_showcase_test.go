//  Copyright 2018 Istio Authors
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

package showcase

import (
	"testing"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/dependency"
)

var cfg = ""

func TestHTTPWithMTLS(t *testing.T) {
	test.Requires(t, dependency.Apps, dependency.Pilot, dependency.MTLS)

	env := test.GetEnvironment(t)
	env.Configure(cfg)

	appa := env.GetApp("a")
	appt := env.GetApp("t")

	reqInfo := appa.Call(appt)

	// Verify that the request was received by t
	if err := appt.Expect(reqInfo); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPWithMTLSAndMixer(t *testing.T) {
	test.Requires(t, dependency.Mixer, dependency.Pilot, dependency.Apps, dependency.MTLS)

	env := test.GetEnvironment(t)

	env.Configure(cfg)

	_ = env.GetPilot()

	m := env.GetMixer()

	appa := env.GetApp("a")
	appt := env.GetApp("t")

	reqInfo := appa.Call(appt)

	// Verify that the request was received by t
	if err := appt.Expect(reqInfo); err != nil {
		t.Fatal(err)
	}

	err := m.Expect(`
CHECK: "a" ....
`)
	if err != nil {
		t.Fatal(err)
	}
}
