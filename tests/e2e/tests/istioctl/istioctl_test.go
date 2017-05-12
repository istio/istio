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
	//    multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/e2e/util"
)

const (
	rulesDir = "tests/apps/istioctl/input"

	// How many retries when waiting for CRUD operation to succeed (database not instantly consistent)
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

	// Customize rule yaml files, replace with actual namespace, and copy to /tmp/<id>
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
			"", "", false); err != nil {
			return err
		}
		if strings.Contains(output, ruleKey) {
			return nil
		}
	}

	return fmt.Errorf("created rule did not exist after %v", time.Duration(retries)*durBetweenAttempts)
}

func waitForRuleRemoval(ruleKey string, retries int, durBetweenAttempts time.Duration) error {

	for retry := 0; retry < retries; retry++ {
		time.Sleep(durBetweenAttempts)
		var output string
		var err error
		if output, err = tc.Kube.Istioctl.RunWithOutput([]string{"get", "route-rules"},
			"", "", false); err != nil {
			return err
		}
		if !strings.Contains(output, ruleKey) {
			return nil
		}
	}

	return fmt.Errorf("created rule still exists after %v", time.Duration(retries)*durBetweenAttempts)
}

// Mixer rules don't have names so we just wait for a rule of the appropriate "kind" (e.g. "quota")
func waitForMixerRuleType(scope, subject, kind string, retries int, durBetweenAttempts time.Duration) error {

	for retry := 0; retry < retries; retry++ {
		time.Sleep(durBetweenAttempts)
		var output string
		var err error
		if output, err = tc.Kube.Istioctl.RunWithOutput([]string{"mixer", "rule", "get", scope, subject},
			"", "", false); err != nil {
			return err
		}
		if strings.Index(output, kind) >= 0 {
			return nil
		}
	}

	return fmt.Errorf("created mixer rule kind did not exist after %v", time.Duration(retries)*durBetweenAttempts)
}

func rulePath(ruleFile string) string {
	return filepath.Join(tc.rulesDir, ruleFile)
}

// TestInvalidRuleDetected verifies attempting to create invalid rule fails and generates an error message explaining the problem
func TestInvalidRuleDetected(t *testing.T) {
	invalidRules := []ruleFileAndCreateResponse{
		{
			ruleFile: "route-rule-101.yaml",
			response: regexp.MustCompile("percent invalid:  must be in range 0..100"),
		},
	}

	for _, invalidRule := range invalidRules {
		output, err := tc.Kube.Istioctl.RunWithOutput([]string{"create", "--file", rulePath(invalidRule.ruleFile)},
			"", "", true)
		if err == nil {
			glog.Error(fmt.Sprintf("Expected failure but got success %v", output))
			t.Fatal(fmt.Sprintf("Expected failure but got success %v", output))
		}
		if !invalidRule.response.Match([]byte(output)) {
			t.Errorf("create file %v did not match %v.  Output: %v", invalidRule.ruleFile, invalidRule.response, output)
		}
	}
}

// TestCRUD tests Create, Read, Update, Delete operations for a rule by performing them and scraping the output.
// Note: If you are improving the output of istioctl, you will have to modify the regexps here used to detect expected output.
func TestCRUD(t *testing.T) {

	const ruleName = "test-100-delay"

	var output string
	var err error
	if output, err = tc.Kube.Istioctl.RunWithOutput([]string{"create", "--file", rulePath("route-rule-100-delay-7s.yaml")},
		"", "", false); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile("Created config: route-rule test-100-delay").Match([]byte(output)) {
		t.Fatalf(fmt.Sprintf("Unexpected create output %s", output))
	}

	if err = waitForRule(ruleName, maxRetries, time.Second); err != nil {
		t.Fatal(err)
	}

	if output, err = tc.Kube.Istioctl.RunWithOutput([]string{"get", "route-rules"},
		"", "", false); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(ruleName).Match([]byte(output)) {
		t.Fatalf(fmt.Sprintf("Unexpected list output %s", output))
	}

	if output, err = tc.Kube.Istioctl.RunWithOutput([]string{"get", "route-rule", ruleName},
		"", "", false); err != nil {
		t.Error(err)
	}
	// The output of a get is YAML, this test only looks for a fragment of it, to verify something was returned from the original
	if !regexp.MustCompile("precedence: 2").Match([]byte(output)) {
		t.Errorf(fmt.Sprintf("Unexpected get output %s", output))
	}

	if output, err = tc.Kube.Istioctl.RunWithOutput([]string{"replace", "--file", rulePath("route-rule-100-delay-700s.yaml")},
		"", "", false); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile("Updated config: route-rule test-100-delay").Match([]byte(output)) {
		t.Fatalf(fmt.Sprintf("Unexpected replace output %s", output))
	}

	// TODO check that the replace above is visible with get/list within maxRetries of time.Second

	// Test that the rule is still there
	if err = waitForRule(ruleName, maxRetries, time.Second); err != nil {
		t.Fatal(err)
	}

	// Attempt to replace with an invalid rule
	if output, err = tc.Kube.Istioctl.RunWithOutput([]string{"replace", "--file", rulePath("route-rule-100-delay-invalid.yaml")},
		"", "", false); err == nil {
		glog.Error(fmt.Sprintf("Expected failure but got success %v", output))
		t.Fatal(fmt.Sprintf("Expected failure but got success %v", output))
	}
	if !regexp.MustCompile("percent invalid:  must be in range 0..100").Match([]byte(output)) {
		t.Fatalf(fmt.Sprintf("Unexpected invalid replace output %s", output))
	}
	// TODO check that the change never shows up?

	if output, err = tc.Kube.Istioctl.RunWithOutput([]string{"delete", "--file", rulePath("route-rule-100-delay-7s.yaml")},
		"", "", false); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile("Deleted config: route-rule test-100-delay").Match([]byte(output)) {
		t.Fatalf(fmt.Sprintf("Unexpected delete output %s", output))
	}

	// Wait for the rule to go away
	if err = waitForRuleRemoval(ruleName, maxRetries, time.Second); err != nil {
		t.Fatal(err)
	}

	// Attempt to delete non-existent rule
	if output, err = tc.Kube.Istioctl.RunWithOutput([]string{"delete", "--file", rulePath("route-rule-100-delay-invalid.yaml")},
		"", "", false); err == nil {
		glog.Error(fmt.Sprintf("Expected failure but got success re-deleting %v", output))
		t.Fatal(fmt.Sprintf("Expected failure but got success re-deleting %v", output))
	}
	if !regexp.MustCompile("not found").Match([]byte(output)) {
		t.Fatalf(fmt.Sprintf("Unexpected invalid delete output %s", output))
	}
}

// TestVersionSubcommand verifies the `istioctl version` subcommand
func TestVersionSubcommand(t *testing.T) {
	output, err := tc.Kube.Istioctl.RunWithOutput([]string{"version"},
		"", "", true)
	if err != nil {
		t.Fatal(err)
	}
	// We don't care what the GitRevision (or other fields) are, just that the command doesn't panic
	if !regexp.MustCompile("GitRevision").Match([]byte(output)) {
		t.Errorf("istioctl version unexpected output: %v", output)
	}
}

// TestCompletionSubcommand verifies the `istioctl completion` subcommand
func TestCompletionSubcommand(t *testing.T) {
	_, err := tc.Kube.Istioctl.RunWithOutput([]string{"completion"},
		"", "", true)
	if err != nil {
		t.Fatal(err)
	}
	// We don't scrape the output, which comes from Cobra.  It is sufficient that the command succeeds.
}

// TestInvalidMixerRuleDetected verifies attempting to create invalid mixer rule fails and generates an error message explaining the problem
func TestInvalidMixerRuleDetected(t *testing.T) {
	invalidRules := []ruleFileAndCreateResponse{
		{
			ruleFile: "mixer-rule-ratings-ratelimit-invalid.yaml",
			// TODO Currently CLI outputs "Error: the server responded with the status code 412 but did not return more information"
			response: regexp.MustCompile("the server responded with"),
		},
	}

	for _, invalidRule := range invalidRules {
		output, err := tc.Kube.Istioctl.RunWithOutput([]string{
			"mixer", "rule", "create",
			"global", "myservice.ns.svc.cluster.local",
			"--file", rulePath(invalidRule.ruleFile)},
			"", "", true)
		if err == nil {
			glog.Error(fmt.Sprintf("Expected failure but got success %v", output))
			t.Fatal(fmt.Sprintf("Expected failure but got success %v", output))
		}
		if invalidRule.response != nil && !invalidRule.response.Match([]byte(output)) {
			t.Errorf("mixer rule create file %v did not match %v.  Output: %v", invalidRule.ruleFile, invalidRule.response, output)
		}
	}
}

// TestMixerCR__ tests Create, Read (but not Update, Delete) operations for a mixer rule by performing them and scraping the output.
// Note: If you are improving the output of istioctl, you will have to modify the regexps here used to detect expected output.
func TestMixerCR__(t *testing.T) {

	scope := "global"
	subject := "myservice.ns.svc.cluster.local"

	var err error
	if _, err = tc.Kube.Istioctl.RunWithOutput([]string{"mixer", "rule", "create", scope, subject, "--file", rulePath("mixer-rule-ratings-ratelimit-5per_s.yaml")},
		"", "", false); err != nil {
		t.Fatal(err)
	}
	// `istioctl mixer rule create` currently has no output on success!  (So we can't scrape it)

	if err := waitForMixerRuleType(scope, subject, "quota", maxRetries, time.Second); err != nil {
		t.Fatal(err)
	}

	// The mixer does support "update" but it is by recreation (mixer rules aren't named!)  So we don't test.

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
