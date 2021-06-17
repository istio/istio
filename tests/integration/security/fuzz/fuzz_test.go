// +build integfuzz
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

package fuzz

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/tests/common/jwt"
)

const (
	apacheServer    = "apache"
	nginxServer     = "nginx"
	dotdotpwn       = "dotdotpwn"
	authzDenyPolicy = `
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata:
  name: policy-deny
spec:
  action: DENY
  rules:
  - to:
    - operation:
        paths: ["/private/secret.html"]
`
	jwtTool            = "jwttool"
	requestAuthnPolicy = `
apiVersion: security.istio.io/v1beta1
kind: RequestAuthentication
metadata:
  name: jwt
spec:
  jwtRules:
  - issuer: "test-issuer-1@istio.io"
    jwksUri: "https://raw.githubusercontent.com/istio/istio/release-1.10/tests/common/jwt/jwks.json"
  - issuer: "test-issuer-2@istio.io"
    jwksUri: "https://raw.githubusercontent.com/istio/istio/release-1.10/tests/common/jwt/jwks.json"
`
)

func deploy(t framework.TestContext, name, ns, yaml string) {
	t.Config().ApplyYAMLOrFail(t, ns, file.AsStringOrFail(t, yaml))
	if _, err := kube.WaitUntilPodsAreReady(kube.NewPodFetch(t.Clusters().Default(), ns, "app="+name)); err != nil {
		t.Fatalf("Wait for pod %s failed: %v", name, err)
	}
	t.Logf("deploy %s is ready", name)
}

func waitService(t framework.TestContext, name, ns string) {
	if _, _, err := kube.WaitUntilServiceEndpointsAreReady(t.Clusters().Default(), ns, name); err != nil {
		t.Fatalf("Wait for service %s failed: %v", name, err)
	}
	t.Logf("service %s is ready", name)
}

func runDotdotPwnTest(t framework.TestContext, ns, server string) {
	pods, err := t.Clusters().Default().PodsForSelector(context.TODO(), ns, "app="+dotdotpwn)
	if err != nil {
		t.Fatalf("failed to get dotdotpwn pod: %v", err)
	}
	t.Logf("running dotdotpwn fuzz test against the %s (should normally complete in 60 seconds)...", server)

	// Run the dotdotpwn fuzz testing against the http server ("-m http") on port 8080 ("-x 8080"), other arguments:
	// "-C": Continue if no data was received from host.
	// "-d 1": depth of traversals.
	// "-t 50": time in milliseconds between each test.
	// "-f private/secret.html": specific filename to fetch after bypassing the authorization policy.
	// "-k secret_data_leaked": text pattern to match in the response.
	// "-r %s.txt": report filename.
	command := fmt.Sprintf(`./run.sh -m http -h %s -x 8080 -C -d 1 -t 50 -f private/secret.html -k secret_data_leaked -r %s.txt`, server, server)
	stdout, stderr, err := t.Clusters().Default().PodExec(pods.Items[0].Name, ns, dotdotpwn, command)
	if err != nil {
		t.Fatalf("failed to run dotdotpwn: %v", err)
	}
	t.Logf("%s\n%s\n", stdout, stderr)
	t.Logf("dotdotpwn fuzz test completed for %s", server)
	if strings.Contains(stdout, "<- VULNERABLE") {
		t.Errorf("found potential policy bypass requests, please read the log for more details")
	} else {
		t.Logf("no potential policy bypass requests found")
	}
}

func TestFuzzAuthorization(t *testing.T) {
	framework.NewTest(t).
		Features("security.fuzz.authorization").
		Run(func(t framework.TestContext) {
			ns := "fuzz-authz"
			namespace.ClaimOrFail(t, t, ns)

			t.Config().ApplyYAMLOrFail(t, ns, authzDenyPolicy)
			t.Logf("authorization policy applied")

			deploy(t, apacheServer, ns, "backends/apache/apache.yaml")
			deploy(t, nginxServer, ns, "backends/nginx/nginx.yaml")
			deploy(t, dotdotpwn, ns, "fuzzers/dotdotpwn/dotdotpwn.yaml")
			waitService(t, apacheServer, ns)
			waitService(t, nginxServer, ns)
			for _, target := range []string{apacheServer, nginxServer} {
				t.NewSubTest(target).Run(func(t framework.TestContext) {
					runDotdotPwnTest(t, ns, target)
				})
			}
		})
}

func runJwtToolTest(t framework.TestContext, ns, server, jwtToken string) {
	pods, err := t.Clusters().Default().PodsForSelector(context.TODO(), ns, "app="+jwtTool)
	if err != nil {
		t.Fatalf("failed to get jwttool pod: %v", err)
	}
	t.Logf("running jwttool fuzz test against the %s (should normally complete in 10 seconds)...", server)

	// Run the jwttool fuzz testing with "--mode at" to run all tests:
	// - JWT Attack Playbook
	// - Fuzz existing claims to force errors
	// - Fuzz common claims
	commands := []string{
		"./run.sh",
		"--targeturl",
		fmt.Sprintf("http://%s:8080/private/secret.html", server),
		"--noproxy",
		"--headers",
		fmt.Sprintf("Authorization: Bearer %s", jwtToken),
		"--mode",
		"at",
	}
	stdout, stderr, err := t.Clusters().Default().PodExecCommands(pods.Items[0].Name, ns, jwtTool, commands)
	if err != nil {
		t.Fatalf("failed to run jwttool: %v", err)
	}
	t.Logf("%s\n%s\n", stdout, stderr)
	t.Logf("jwttool fuzz test completed for %s", server)

	if !strings.Contains(stdout, "Prescan: original token Response Code: 200") {
		t.Fatalf("could not find prescan check, please make sure the jwt_tool.py completed successfully")
	}
	errCases := []string{}
	scanStarted := false
	for _, line := range strings.Split(stdout, "\n") {
		if scanStarted {
			// First check the response is a valid test case.
			if strings.Contains(line, "jwttool_") && !strings.Contains(line, "(should always be valid)") {
				// Then add it to errCases if the test case has a response code other than 401.
				if !strings.Contains(line, "Response Code: 401") {
					errCases = append(errCases, line)
				}
			}
		} else if strings.Contains(line, "LAUNCHING SCAN") {
			scanStarted = true
		}
	}
	if len(errCases) != 0 {
		t.Errorf("found %d potential policy bypass requests:\n- %s", len(errCases), strings.Join(errCases, "\n- "))
	} else {
		t.Logf("no potential policy bypass requests found")
	}
}

func TestRequestAuthentication(t *testing.T) {
	framework.NewTest(t).
		Features("security.fuzz.jwt").
		Run(func(t framework.TestContext) {
			ns := "fuzz-jwt"
			namespace.ClaimOrFail(t, t, ns)

			t.Config().ApplyYAMLOrFail(t, ns, requestAuthnPolicy)
			t.Logf("request authentication policy applied")

			// We don't care about the actual backend for JWT test, one backend is good enough.
			deploy(t, apacheServer, ns, "backends/apache/apache.yaml")
			deploy(t, jwtTool, ns, "fuzzers/jwt_tool/jwt_tool.yaml")
			waitService(t, apacheServer, ns)
			testCases := []struct {
				name      string
				baseToken string
			}{
				{"TokenIssuer1", jwt.TokenIssuer1},
				{"TokenIssuer1WithAud", jwt.TokenIssuer1WithAud},
				{"TokenIssuer1WithAzp", jwt.TokenIssuer1WithAzp},
				{"TokenIssuer2", jwt.TokenIssuer2},
				{"TokenIssuer1WithNestedClaims1", jwt.TokenIssuer1WithNestedClaims1},
				{"TokenIssuer1WithNestedClaims2", jwt.TokenIssuer1WithNestedClaims2},
				{"TokenIssuer2WithSpaceDelimitedScope", jwt.TokenIssuer2WithSpaceDelimitedScope},
			}
			for _, tc := range testCases {
				t.NewSubTest(tc.name).Run(func(t framework.TestContext) {
					runJwtToolTest(t, ns, apacheServer, tc.baseToken)
				})
			}
		})
}
