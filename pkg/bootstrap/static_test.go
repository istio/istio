// Copyright 2018 Istio Authors
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

package bootstrap_test

import (
	"io/ioutil"
	"testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/bootstrap"
)

func TestBuildBootstrap(t *testing.T) {
	noauth := meshconfig.ProxyConfig{
		ControlPlaneAuthPolicy: meshconfig.AuthenticationPolicy_NONE,
		ZipkinAddress:          "zipkin:9411",
		ProxyAdminPort:         15000,
	}
	auth := noauth
	auth.ControlPlaneAuthPolicy = meshconfig.AuthenticationPolicy_MUTUAL_TLS

	telemetryAuth, err := bootstrap.BuildBootstrap(bootstrap.BuildOptions(auth, "telemetry", "pod1", "ns2"))
	if err != nil {
		t.Error(err)
	}

	got, err := bootstrap.ToYAML(telemetryAuth)
	if err != nil {
		t.Error(err)
	}
	want, err := ioutil.ReadFile("testdata/telemetry-auth.yaml")
	if err != nil {
		t.Error(err)
	}
	if string(got) != string(want) {
		t.Errorf("telemetry auth bootstrap changed: got\n%s", string(got))
	}

	if _, err = bootstrap.BuildBootstrap(bootstrap.BuildOptions(auth, "policy", "pod2", "ns3")); err != nil {
		t.Error(err)
	}
	if _, err = bootstrap.BuildBootstrap(bootstrap.BuildOptions(noauth, "policy", "pod2", "ns3")); err != nil {
		t.Error(err)
	}
}
