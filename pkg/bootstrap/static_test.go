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
	"fmt"
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

	cases := []struct {
		opts bootstrap.Options
		file string
	}{
		{
			opts: bootstrap.BuildOptions(auth, "telemetry", "pod1", "ns2"),
			file: "testdata/telemetry-auth.yaml",
		},
		{
			opts: bootstrap.BuildOptions(auth, "policy", "pod2", "ns3"),
			file: "testdata/policy-auth.yaml",
		},
		{
			opts: bootstrap.BuildOptions(noauth, "policy", "pod2", "ns3"),
			file: "testdata/policy-noauth.yaml",
		},
		{
			opts: bootstrap.BuildOptions(auth, "pilot", "pod3", "ns4"),
			file: "testdata/pilot-auth.yaml",
		},
		{
			opts: bootstrap.BuildOptions(noauth, "pilot", "pod3", "ns4"),
			file: "testdata/pilot-noauth.yaml",
		},
	}

	for _, test := range cases {
		t.Run(fmt.Sprintf("bootstrap gen for %s", test.file), func(t *testing.T) {
			config, err := bootstrap.BuildBootstrap(test.opts)
			if err != nil {
				t.Error(err)
			}
			got, err := bootstrap.ToYAML(config)
			if err != nil {
				t.Error(err)
			}
			want, err := ioutil.ReadFile(test.file)
			if err != nil {
				t.Error(err)
			}
			if string(got) != string(want) {
				t.Errorf("%s bootstrap changed: got\n%s", test.file, string(got))
			}
		})
	}
}
