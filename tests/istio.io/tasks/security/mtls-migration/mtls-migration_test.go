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
package tests

import (
	"fmt"
	"strings"
	"testing"

	"istio.io/istio/pkg/test/framework"
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/istio.io/examples"
)

var (
	ist istio.Instance
)

func TestMain(m *testing.M) {
	framework.NewSuite("mtls-migration", m).
		SetupOnEnv(environment.Kube, istio.Setup(&ist, nil)).
		RequireEnvironment(environment.Kube).
		Run()
}

func validateInitialPolicies(output string) error {
	//verify that only the following exist:
	// NAMESPACE      NAME                          AGE
	// istio-system   grafana-ports-mtls-disabled   3m

	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return fmt.Errorf("expected output to be 2 lines; actual: %d", len(lines))
	}

	parts := strings.Split(lines[1], " ")
	if len(parts) != 2 {
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

func validateInitialDestinationRules(output string) error {
	//verify that only the following exists:
	//NAMESPACE      NAME              AGE
	//istio-system   istio-policy      25m
	//istio-system   istio-telemetry   25m

	lines := strings.Split(output, "\n")
	if len(lines) < 3 {
		return fmt.Errorf("expected output to be 3 lines; actual: %d", len(lines))
	}

	line := lines[1]
	parts := strings.Split(line, " ")
	if len(parts) != 2 {
		return fmt.Errorf("expected output to follow namespace, name, age; actual was: %s", line)
	}

	if parts[0] != "istio-system" {
		return fmt.Errorf("expected namespace to be istio-system; actual: %s", parts[0])
	}

	if parts[1] != "istio-policy" {
		return fmt.Errorf("expected name to be istio-policy; actual: %s", parts[1])
	}

	line = lines[2]
	parts = strings.Split(line, " ")
	if parts[0] != "istio-system" {
		return fmt.Errorf("expected namespace to be istio-system; actual: %s", parts[0])
	}

	if parts[1] != "istio-policy" {
		return fmt.Errorf("expected name to be istio-telemetry; actual: %s", parts[2])
	}

	return nil
}

//https://istio.io/docs/tasks/security/mtls-migration/
//https://github.com/istio/istio.io/blob/release-1.2/content/docs/tasks/security/mtls-migration/index.md
func TestMTLS(t *testing.T) {
	//Test
	examples.New(t, "mtls-migration").
		RunScript("create-ns-foo-bar-legacy.sh", examples.TextOutput, nil).
		WaitForPods(examples.NewMultiPodFetch("foo")).
		WaitForPods(examples.NewMultiPodFetch("bar")).
		WaitForPods(examples.NewMultiPodFetch("legacy")).
		RunScript("curl-foo-bar-legacy.sh", examples.TextOutput, examples.GetCurlVerifier([]string{"200", "200", "200"})).
		//RunScript("verify-initial-policies.sh", examples.TextOutput, validateInitialPolicies).
		//RunScript("verify-initial-destinationrules.sh", examples.TextOutput, validateInitialDestinationRules).
		RunScript("configure-mtls-destinationrule.sh", examples.TextOutput, nil).
		RunScript("curl-foo-bar-legacy_post_dr.sh", examples.TextOutput, examples.GetCurlVerifier([]string{"200", "200", "200"})).
		RunScript("httpbin-foo-mtls-only.sh", examples.TextOutput, nil).
		RunScript("curl-foo-bar-legacy_httpbin_foo_mtls.sh", examples.TextOutput,
			examples.GetCurlVerifier([]string{"200", "200", "000", "command terminated with exit code 56"})).
		RunScript("cleanup.sh", examples.TextOutput, nil).
		Run()
}
