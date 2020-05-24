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

package cmd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/env"
	pilot_util "istio.io/istio/tests/util"
)

func TestProxyStatusV2(t *testing.T) {
	_, tearDown := pilot_util.EnsureTestServer(func(args *bootstrap.PilotArgs) {
		// TODO remove any not needed or switch to something that will let istio.io/connections have data?
		// TODO Verify this won't pull from my true cluster
		args.Plugins = bootstrap.DefaultPlugins
		args.Config.FileDir = env.IstioSrc + "/tests/testdata/networking/sidecar-ns-scope"
		args.Mesh.MixerAddress = ""
		args.MeshConfig = nil
		args.Mesh.ConfigFile = env.IstioSrc + "/tests/testdata/networking/sidecar-ns-scope/mesh.yaml"
		args.Service.Registries = []string{}
	})
	defer tearDown()

	// TODO This fails with warn, ads	ADS: Unknown watched resources
	// node:<id:"sidecar~1.2.3.4~test-1.default~default.svc.cluster.local" ...
	// Why?
	cases := []execTestCase{
		{ // case 0
			args:           strings.Split(fmt.Sprintf("experimental proxy-status --endpoint %s", pilot_util.MockPilotGrpcAddr), " "),
			expectedString: "NAME     CDS     LDS     EDS     RDS     PILOT",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, strings.Join(c.args, " ")), func(t *testing.T) {
			verifyGrpcTestOutput(t, c)
		})
	}
}

func verifyGrpcTestOutput(t *testing.T, c execTestCase) {
	t.Helper()

	var out bytes.Buffer
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOutput(&out)

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}

	if c.expectedString != "" && !strings.Contains(output, c.expectedString) {
		t.Fatalf("Output didn't match for 'istioctl %s'\n got %v\nwant: %v", strings.Join(c.args, " "), output, c.expectedString)
	}

	if c.goldenFilename != "" {
		util.CompareContent([]byte(output), c.goldenFilename, t)
	}

	if c.wantException {
		if fErr == nil {
			t.Fatalf("Wanted an exception for 'istioctl %s', didn't get one, output was %q",
				strings.Join(c.args, " "), output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception for 'istioctl %s': %v", strings.Join(c.args, " "), fErr)
		}
	}
}
