//go:build integfuzz
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
	"istio.io/istio/tests/common/jwt"
)

const (
	apacheServer = "apache"
	nginxServer  = "nginx"
	tomcatServer = "tomcat"

	dotdotpwn = "dotdotpwn"
	wfuzz     = "wfuzz"
	ffuf      = "ffuf"

	authzDenyPolicy = `
apiVersion: security.istio.io/v1
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
apiVersion: security.istio.io/v1
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

var (
	// Known unsupported path parameter ("/bla;foo") normalization for Tomcat.
	dotdotPwnIgnoreTomcat = []string{
		"/../private/secret.html;index.html <- VULNERABLE!",
		"/../private/secret.html;index.htm <- VULNERABLE!",
		"/..%5Cprivate%5Csecret.html;index.html <- VULNERABLE!",
		"/..%5Cprivate%5Csecret.html;index.htm <- VULNERABLE!",
	}
	// Known unsupported path parameter ("/bla;foo") normalization for Tomcat.
	wfuzzIgnoreTomcat = []string{
		`%2e%2e/%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e/%2e%2e/%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e/%2e%2e/%2e%2e/%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e/%2e%2e/%2e%2e/%2e%2e/%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%2e%2e/%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e\\%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e\\%2e%2e\\%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e\\%2e%2e\\%2e%2e\\%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
		`%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%2e%2e\\%70%72%69%76%61%74%65/%73%65%63%72%65%74%2e%68%74%6d%6c;%69%6e%64%65%78%2e%68%74%6d%6c`,
	}
)

func deploy(t framework.TestContext, name, ns, yaml string) {
	t.ConfigIstio().File(ns, yaml).ApplyOrFail(t)
	if _, err := kube.WaitUntilPodsAreReady(kube.NewPodFetch(t.Clusters().Default(), ns, "app="+name)); err != nil {
		t.Fatalf("Wait for pod %s failed: %v", name, err)
	}
	t.Logf("deploy %s is ready", name)
}

func waitService(t framework.TestContext, name, ns string) {
	if _, _, err := kube.WaitUntilServiceEndpointsAreReady(t.Clusters().Default().Kube(), ns, name); err != nil {
		t.Fatalf("Wait for service %s failed: %v", name, err)
	}
	t.Logf("service %s is ready", name)
}

func ignoreTomcat(t framework.TestContext, line string, ignores []string) bool {
	for _, ignore := range ignores {
		if strings.Contains(line, ignore) {
			t.Logf("ignored known unsupported normalization: %s", ignore)
			return true
		}
	}
	return false
}

func runFuzzer(t framework.TestContext, fuzzer, ns, server string) {
	pods, err := t.Clusters().Default().PodsForSelector(context.TODO(), ns, "app="+fuzzer)
	if err != nil {
		t.Fatalf("failed to get %s pod: %v", fuzzer, err)
	}
	t.Logf("running %s test against the %s (should normally complete in 60 seconds)...", fuzzer, server)

	switch fuzzer {
	case dotdotpwn:
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

		var errLines []string
		for _, line := range strings.Split(stdout, "\n") {
			if strings.Contains(line, "<- VULNERABLE") {
				if server == tomcatServer && ignoreTomcat(t, line, dotdotPwnIgnoreTomcat) {
					continue
				}
				errLines = append(errLines, line)
			}
		}
		if len(errLines) != 0 {
			t.Errorf("found potential policy bypass requests, please read the log for more details:\n- %s", strings.Join(errLines, "\n- "))
		} else {
			t.Logf("no potential policy bypass requests found")
		}
	case ffuf:
		command := fmt.Sprintf("/ffuf/run.sh -w /wordlist/dirTraversal.txt -u http://%s:8080/FUZZ -noninteractive -mc 200 -o /ffuf/output-log", server)
		stdout, _, err := t.Clusters().Default().PodExec(pods.Items[0].Name, ns, ffuf, command)
		if err != nil {
			t.Fatalf("failed to run ffuf: %v", err)
		}
		var errLines []string
		for _, line := range strings.Split(stdout, "\n") {
			if strings.Contains(line, "Status: 200") {
				// Test the bypass with curl:
				payload := strings.Split(line, " [Status: 200,")[0]
				command2 := fmt.Sprintf("curl http://%s:8080/%s", server, payload)
				stdout2, _, _ := t.Clusters().Default().PodExec(pods.Items[0].Name, ns, ffuf, command2)
				if strings.Contains(stdout2, "secret_data_leaked") {
					errLines = append(errLines, payload)
				}
			}
		}
		if len(errLines) != 0 {
			t.Errorf("found potential policy bypass requests:\n- %s", strings.Join(errLines, "\n- "))
		} else {
			t.Logf("no potential policy bypass requests found")
		}
	case wfuzz:
		// Run the wfuzz fuzz with the following parameters:
		// -z file,wordlist/dirTraversal.txt,<...>: Fuzz based on the basic directory traversal patterns with various encodings (see details below).
		// -f %s.out,csv -o csv: Output result in csv format.
		// --ss secret_data_leaked: Show responses with the specified regex within the content.
		// -t 5: Specify the number of concurrent connections.
		// %s:8080/FUZZ: target server.
		command := fmt.Sprintf("wfuzz -z file,wordlist/dirTraversal.txt,"+
			"doble_nibble_hex-"+ // Replaces ALL characters in string using the %%dd%dd escape.
			"double_urlencode-"+ // Applies a double encode to special characters in string using the %25xx escape.
			// Letters, digits, and the characters '_.-' are never quoted.
			"first_nibble_hex-"+ // Replaces ALL characters in string using the %%dd? escape.
			"second_nibble_hex-"+ // Replaces ALL characters in string using the %?%dd escape.
			"uri_double_hex-"+ // Encodes ALL characters using the %25xx escape.
			"uri_hex-"+ // Encodes ALL characters using the %xx escape.
			"uri_triple_hex-"+ // Encodes ALL characters using the %25%xx%xx escape.
			"uri_unicode-"+ // Replaces ALL characters in string using the %u00xx escape.
			"urlencode-"+ // Replace special characters in string using the %xx escape.
			// Letters, digits, and the characters '_.-' are never quoted.
			"utf8 "+ // Replaces ALL characters in string using the \u00xx escape.
			"-f %s.out,csv -o csv -c -v --ss secret_data_leaked -t 5 %s:8080/FUZZ", server, server)
		stdout, stderr, err := t.Clusters().Default().PodExec(pods.Items[0].Name, ns, wfuzz, command)
		if err != nil {
			t.Fatalf("failed to run wfuzz: %v", err)
		}
		t.Logf("%s\n%s\n", stdout, stderr)
		t.Logf("wfuzz test completed for %s", server)

		var errLines []string
		for _, line := range strings.Split(stdout, "\n") {
			if strings.Contains(line, ",200,") {
				if server == tomcatServer && ignoreTomcat(t, line, wfuzzIgnoreTomcat) {
					continue
				}
				errLines = append(errLines, line)
			}
		}
		if len(errLines) != 0 {
			t.Errorf("found potential policy bypass requests, please read the log for more details:\n- %s", strings.Join(errLines, "\n- "))
		} else {
			t.Logf("no potential policy bypass requests found")
		}

	default:
		t.Fatalf("unknown fuzzer %s", fuzzer)
	}
}

func TestFuzzAuthorization(t *testing.T) {
	framework.NewTest(t).
		Run(func(t framework.TestContext) {
			ns := "fuzz-authz"
			namespace.ClaimOrFail(t, ns)

			t.ConfigIstio().YAML(ns, authzDenyPolicy).ApplyOrFail(t)
			t.Logf("authorization policy applied")

			deploy(t, dotdotpwn, ns, "fuzzers/dotdotpwn/dotdotpwn.yaml")
			t.ConfigIstio().File(ns, "fuzzers/wfuzz/wordlist.yaml").ApplyOrFail(t)
			deploy(t, wfuzz, ns, "fuzzers/wfuzz/wfuzz.yaml")
			t.ConfigIstio().File(ns, "fuzzers/ffuf/wordlist.yaml").ApplyOrFail(t)
			deploy(t, ffuf, ns, "fuzzers/ffuf/ffuf.yaml")

			deploy(t, apacheServer, ns, "backends/apache/apache.yaml")
			deploy(t, nginxServer, ns, "backends/nginx/nginx.yaml")
			deploy(t, tomcatServer, ns, "backends/tomcat/tomcat.yaml")
			waitService(t, apacheServer, ns)
			waitService(t, nginxServer, ns)
			waitService(t, tomcatServer, ns)
			for _, fuzzer := range []string{dotdotpwn, ffuf, wfuzz} {
				t.NewSubTest(fuzzer).Run(func(t framework.TestContext) {
					for _, target := range []string{apacheServer, nginxServer, tomcatServer} {
						t.NewSubTest(target).Run(func(t framework.TestContext) {
							runFuzzer(t, fuzzer, ns, target)
						})
					}
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
		t.Fatal("could not find prescan check, please make sure the jwt_tool.py completed successfully")
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
		Run(func(t framework.TestContext) {
			ns := "fuzz-jwt"
			namespace.ClaimOrFail(t, ns)

			t.ConfigIstio().YAML(ns, requestAuthnPolicy).ApplyOrFail(t)
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
