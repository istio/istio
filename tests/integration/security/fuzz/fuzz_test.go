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
)

func deploy(t framework.TestContext, name, ns, yaml string) {
	t.Config().ApplyYAMLOrFail(t, ns, file.AsStringOrFail(t, yaml))
	if _, err := kube.WaitUntilPodsAreReady(kube.NewPodFetch(t.Clusters().Default(), ns, "app="+name)); err != nil {
		t.Fatalf("Wait for pod %s failed: %v", name, err)
	}
	if name != dotdotpwn {
		if _, _, err := kube.WaitUntilServiceEndpointsAreReady(t.Clusters().Default(), ns, name); err != nil {
			t.Fatalf("Wait for service %s failed: %v", name, err)
		}
	}
	t.Logf("%s is ready", name)
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
			for _, target := range []string{apacheServer, nginxServer} {
				t.NewSubTest(target).Run(func(t framework.TestContext) {
					runDotdotPwnTest(t, ns, target)
				})
			}
		})
}
