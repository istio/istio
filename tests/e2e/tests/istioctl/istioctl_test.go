// Copyright 2017 Istio Authors
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

package e2e

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"

	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/e2e/util"
)

const (
	rulesDir = "tests/apps/istioctl/input"

	// How many retries when waiting for CRUD operation to succeed (database
	// not instantly consistent)
	maxRetries = 5
)

var (
	tc        *testConfig
	ruleFiles = []string{
		"route-rule-101.yaml", // An invalid rule

		"route-rule-100-delay-7s.yaml",
		"route-rule-100-delay-700s.yaml",
		"route-rule-100-delay-invalid.yaml", // invalid rule

		"mixer-rule-ratings-ratelimit-5per_s.yaml",
		"mixer-rule-ratings-ratelimit-invalid.yaml",
	}
)

type testConfig struct {
	*framework.CommonConfig
	gateway  string
	rulesDir string
}

type ruleFileAndCreateResponse struct {
	ruleFile string
	response *regexp.Regexp
}

func (t *testConfig) Setup() error {
	t.gateway = "http://" + tc.Kube.Ingress

	// Customize rule yaml files, replace with actual namespace,
	// and copy to /tmp/<id>
	for _, rule := range ruleFiles {
		src := util.GetResourcePath(filepath.Join(rulesDir, rule))
		dest := filepath.Join(t.rulesDir, rule)
		orig, err := ioutil.ReadFile(src)
		if err != nil {
			glog.Errorf("Failed to read original rule file %s", src)
			return err
		}
		content := string(orig)
		content = strings.Replace(content, "default", t.Kube.Namespace, -1)
		err = ioutil.WriteFile(dest, []byte(content), 0600)
		if err != nil {
			glog.Errorf("Failed to write into new rule file %s", dest)
			return err
		}
	}

	return nil
}

func (t *testConfig) Teardown() error {
	return nil
}

func waitForRule(ruleKey string, retries int, durBetweenAttempts time.Duration) error {

	for retry := 0; retry < retries; retry++ {
		time.Sleep(durBetweenAttempts)
		var output string
		var err error
		if output, err = tc.Kube.Istioctl.RunWithOutput([]string{"get", "route-rules"},
			"", ""); err != nil {
			return err
		}
		if strings.Contains(output, ruleKey) {
			return nil
		}
	}

	return fmt.Errorf("created rule did not exist after %v",
		time.Duration(retries)*durBetweenAttempts)
}

func waitForRuleRemoval(ruleKey string, retries int, durBetweenAttempts time.Duration) error {

	for retry := 0; retry < retries; retry++ {
		time.Sleep(durBetweenAttempts)
		var output string
		var err error
		if output, err = tc.Kube.Istioctl.RunWithOutput(
			[]string{"get", "route-rules"}, "", ""); err != nil {
			return err
		}
		if !strings.Contains(output, ruleKey) {
			return nil
		}
	}

	return fmt.Errorf("created rule still exists after %v",
		time.Duration(retries)*durBetweenAttempts)
}

// Mixer rules don't have names so we just wait for a rule of the appropriate
// "kind" (e.g. "quota")
func waitForMixerRuleType(scope, subject, kind string, retries int, durBetweenAttempts time.Duration) error {

	for retry := 0; retry < retries; retry++ {
		time.Sleep(durBetweenAttempts)
		var output string
		var err error
		if output, err = tc.Kube.Istioctl.RunWithOutput(
			[]string{"mixer", "rule", "get", scope, subject}, "", ""); err != nil {
			return err
		}
		if strings.Contains(output, kind) {
			return nil
		}
	}

	return fmt.Errorf("created mixer rule kind did not exist after %v",
		time.Duration(retries)*durBetweenAttempts)
}

func rulePath(ruleFile string) string {
	return filepath.Join(tc.rulesDir, ruleFile)
}

// Run `istioctl` and match output against a pattern
func istioctlSucceedAndMatchString(args []string, pattern string, t *testing.T) {

	if output, err := tc.Kube.Istioctl.RunWithOutput(args, "", ""); err != nil {
		t.Fatal(err)
	} else {

		if !regexp.MustCompile(pattern).Match([]byte(output)) {
			t.Fatalf(fmt.Sprintf("Unexpected 'istioctl %s' output %s",
				strings.Join(args, " "), output))
		}
	}
}

// Run `istioctl` (INSISTING ON FAILURE) and match output against a pattern
func istioctlFailAndMatchString(args []string, pattern string, t *testing.T) {
	istioctlFailAndMatch(args, regexp.MustCompile(pattern), t)
}

// Run `istioctl` (INSISTING ON FAILURE) and match output against a pattern
func istioctlFailAndMatch(args []string, pattern *regexp.Regexp, t *testing.T) {

	if output, err := tc.Kube.Istioctl.RunWithOutput(args, "", ""); err == nil {
		t.Errorf(fmt.Sprintf("'istioctl %s' is required to fail but it succeeded",
			strings.Join(args, " ")))
	} else {

		if !pattern.Match([]byte(output)) {
			t.Fatalf(fmt.Sprintf("Unexpected 'istioctl %s' output %s",
				strings.Join(args, " "), output))
		}
	}
}

// TestInvalidRuleDetected verifies attempting to create invalid rule fails and
// generates an error message explaining the problem
func TestInvalidRuleDetected(t *testing.T) {
	invalidRules := []ruleFileAndCreateResponse{
		{
			ruleFile: "route-rule-101.yaml",
			response: regexp.MustCompile("percent invalid:  must be in range 0..100"),
		},
	}

	for _, invalidRule := range invalidRules {
		istioctlFailAndMatch(
			[]string{"create", "--file", rulePath(invalidRule.ruleFile)},
			invalidRule.response, t)
	}
}

// TestCRUD tests Create, Read, Update, Delete operations for a rule by
// performing them and scraping the output.
// Note: If you are improving the output of istioctl, you will have to modify
// the regexps here used to detect expected output.
func TestCRUD(t *testing.T) {

	const ruleName = "test-100-delay"

	// Verify apiserver accepts our create and istioctl reports it
	istioctlSucceedAndMatchString(
		[]string{"create", "--file", rulePath("route-rule-100-delay-7s.yaml")},
		"Created config: route-rule test-100-delay", t)

	// Verify the rule was really added to apiserver
	if err := waitForRule(ruleName, maxRetries, time.Second); err != nil {
		t.Fatal(err)
	}

	// Verify the list of route-rules displayed includes the new rule
	istioctlSucceedAndMatchString(
		[]string{"get", "route-rules"},
		ruleName, t)

	// Verify we can get YAML for the rule we created
	istioctlSucceedAndMatchString(
		[]string{"get", "route-rule", ruleName},
		"precedence: 2", t) // Match just against a fragment of YAML

	// Verify we can update a rule
	istioctlSucceedAndMatchString(
		[]string{"replace", "--file", rulePath("route-rule-100-delay-700s.yaml")},
		"Updated config: route-rule test-100-delay", t)

	// Test that the rule is still there
	if err := waitForRule(ruleName, maxRetries, time.Second); err != nil {
		t.Fatal(err)
	}

	// Attempt to replace with an invalid rule
	istioctlFailAndMatchString(
		[]string{"replace", "--file", rulePath("route-rule-100-delay-invalid.yaml")},
		"percent invalid:  must be in range 0..100", t)

	// apiserver must accept the delete and istioctl must report that
	istioctlSucceedAndMatchString(
		[]string{"delete", "--file", rulePath("route-rule-100-delay-7s.yaml")},
		"Deleted config: route-rule test-100-delay", t)

	// Wait for the rule to go away
	if err := waitForRuleRemoval(ruleName, maxRetries, time.Second); err != nil {
		t.Fatal(err)
	}

	// Attempt to delete non-existent rule
	istioctlFailAndMatchString(
		[]string{"delete", "route-rule", "test-100-delay"},
		"not found", t)
}

// TestVersionSubcommand verifies the `istioctl version` subcommand
func TestVersionSubcommand(t *testing.T) {

	// We don't care what the GitRevision (or other fields) are, just that the
	// command doesn't panic
	istioctlSucceedAndMatchString(
		[]string{"version"},
		"GitRevision", t)
}

// TestCompletionSubcommand verifies the `istioctl completion` subcommand
func TestCompletionSubcommand(t *testing.T) {

	// We don't scrape the output, which comes from Cobra.  It is sufficient
	// that the command succeeds.
	istioctlSucceedAndMatchString(
		[]string{"completion"},
		"", t)
}

// TestInvalidMixerRuleDetected verifies attempting to create invalid mixer
// rule fails and generates an error message explaining the problem
func TestInvalidMixerRuleDetected(t *testing.T) {
	invalidRules := []ruleFileAndCreateResponse{
		{
			ruleFile: "mixer-rule-ratings-ratelimit-invalid.yaml",
			// TODO Currently CLI outputs "Error: the server responded with the
			// status code 412 but did not return more information"
			response: regexp.MustCompile("maxAmount: must be >= 0"),
		},
	}

	for _, invalidRule := range invalidRules {
		istioctlFailAndMatch(
			[]string{"mixer", "rule", "create",
				"global", "myservice.ns.svc.cluster.local",
				"--file", rulePath(invalidRule.ruleFile)},
			invalidRule.response, t)
	}
}

// TestMixerCR__ tests Create, Read (but not Update, Delete) operations for a
// mixer rule by performing them and scraping the output.
// Note: If you are improving the output of istioctl, you will have to modify
// the regexps here used to detect expected output.
func TestMixerCR__(t *testing.T) {

	scope := "global"
	subject := "myservice.ns.svc.cluster.local"

	// `istioctl mixer rule create` currently has no output on success!
	// (So we can't scrape it)
	istioctlSucceedAndMatchString(
		[]string{"mixer", "rule", "create", scope, subject,
			"--file", rulePath("mixer-rule-ratings-ratelimit-5per_s.yaml")},
		"", t)

	if err := waitForMixerRuleType(scope, subject, "quota", maxRetries, time.Second); err != nil {
		t.Fatal(err)
	}

	// The mixer does support "update" but it is by recreation (mixer
	// rules aren't named!)  So we don't test.

	// The mixer CLI doesn't support delete (!!!) so we can't test it.
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("istioctl_test")
	if err != nil {
		return err
	}
	tc = new(testConfig)
	tc.CommonConfig = cc
	tc.rulesDir, err = ioutil.TempDir(os.TempDir(), "istioctl_test")
	return err
}

func TestMain(m *testing.M) {
	flag.Parse()
	if err := framework.InitGlog(); err != nil {
		glog.Fatalf("cannot setup glog: %v", err.Error())
	}
	if err := setTestConfig(); err != nil {
		glog.Fatalf("could not create TestConfig: %v", err.Error())
	}
	tc.Cleanup.RegisterCleanable(tc)
	os.Exit(tc.RunTest(m))
}
