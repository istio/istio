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

package istioio

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/istioio"
)

var (
	spaces = regexp.MustCompile(`\s+`)
)

func getLineParts(line string) []string {
	//remove any extra spaces from the line before splitting
	line = spaces.ReplaceAllString(line, " ")
	return strings.Split(line, " ")
}

func validateInitialPolicies(output string, _ error) error {
	//verify that only the following exist:
	// NAMESPACE      NAME                          AGE
	// istio-system   grafana-ports-mtls-disabled   3m

	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return fmt.Errorf("expected output to be 2 lines; actual: %d", len(lines))
	}

	parts := getLineParts(lines[1])
	if len(parts) != 3 {
		return fmt.Errorf("expected output to follow: namespace, name, age; actual was: %s", lines[1])
	}

	if parts[0] != "istio-system" {
		return fmt.Errorf("expected namespace to be istio-system actual: %s", parts[0])
	}

	if parts[1] != "grafana-ports-mtls-disabled" {
		return fmt.Errorf("expected policies to be grafana-ports-mtls-disabled actual: %s", parts[1])
	}

	return nil
}

func validateInitialDestinationRules(output string, _ error) error {
	//verify that only the following exists:
	//NAMESPACE      NAME             HOST                                            AGE
	//istio-system   istio-policy    istio-policy.istio-system.svc.cluster.local      25m
	//istio-system   istio-telemetry istio-telemetry.istio-system.svc.cluster.local    25m

	lines := strings.Split(output, "\n")
	if len(lines) < 3 {
		return fmt.Errorf("expected output to be 3 lines; actual: %d", len(lines))
	}

	parts := getLineParts(lines[1])
	if len(parts) != 4 {
		return fmt.Errorf("expected istio-policy output to follow namespace, name, host, age; actual was: %s", lines[1])
	}

	if parts[0] != "istio-system" {
		return fmt.Errorf("expected namespace to be istio-system; actual: %s", parts[0])
	}

	if parts[1] != "istio-policy" {
		return fmt.Errorf("expected name to be istio-policy; actual: %s", parts[1])
	}

	parts = getLineParts(lines[2])
	if len(parts) != 4 {
		return fmt.Errorf("expected istio-telemetry output to follow namespace, name, host, age; actual was: %s", lines[2])
	}
	if parts[0] != "istio-system" {
		return fmt.Errorf("expected namespace to be istio-system; actual: %s", parts[0])
	}

	if parts[1] != "istio-telemetry" {
		return fmt.Errorf("expected name to be istio-telemetry; actual: %s", parts[1])
	}

	return nil
}

//https://istio.io/docs/tasks/security/mtls-migration/
//https://github.com/istio/istio.io/blob/release-1.2/content/docs/tasks/security/mtls-migration/index.md
func TestMTLSMigration(t *testing.T) {
	framework.
		NewTest(t).
		Run(istioio.NewBuilder().
			RunScript(istioio.File("scripts/mtls/create-ns-foo-bar-legacy.sh"), nil).
			WaitForPods(istioio.NewMultiPodFetch("foo")).
			WaitForPods(istioio.NewMultiPodFetch("bar")).
			WaitForPods(istioio.NewMultiPodFetch("legacy")).
			RunScript(istioio.File("scripts/mtls/curl-foo-bar-legacy.sh"), istioio.CurlVerifier("200", "200", "200")).
			RunScript(istioio.File("scripts/mtls/verify-initial-policies.sh"), validateInitialPolicies).
			RunScript(istioio.File("scripts/mtls/verify-initial-destinationrules.sh"), validateInitialDestinationRules).
			RunScript(istioio.File("scripts/mtls/configure-mtls-destinationrule.sh"), nil).
			RunScript(istioio.File("scripts/mtls/curl-foo-bar-legacy_post_dr.sh"), istioio.CurlVerifier("200", "200", "200")).
			RunScript(istioio.File("scripts/mtls/httpbin-foo-mtls-only.sh"), nil).
			RunScript(istioio.File("scripts/mtls/curl-foo-bar-legacy_httpbin_foo_mtls.sh"),
				istioio.CurlVerifier("200", "200", "000", "command terminated with exit code 56")).
			RunScript(istioio.File("scripts/mtls/cleanup.sh"), nil).
			Build())
}
