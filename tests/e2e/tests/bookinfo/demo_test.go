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
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"
	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/e2e/util"
)

const (
	u1           = "normal-user"
	u2           = "test-user"
	create       = "create"
	bookinfoYaml = "samples/apps/bookinfo/bookinfo.yaml"
	modelDir     = "tests/apps/bookinfo/output"
	rulesDir     = "samples/apps/bookinfo"
	allRule      = "route-rule-all-v1.yaml"
	delayRule    = "route-rule-delay.yaml"
	fiftyRule    = "route-rule-reviews-50-v3.yaml"
	testRule     = "route-rule-reviews-test-v2.yaml"
        testDbRule   = "route-rule-ratings-mysql.yaml"
)

var (
	tc             *testConfig
	testRetryTimes = 10
	defaultRules   = []string{allRule}
)

type testConfig struct {
	*framework.CommonConfig
	gateway  string
	rulesDir string
}

func getWithCookie(url string, cookies []http.Cookie) (*http.Response, error) {
	// Declare http client
	client := &http.Client{}

	// Declare HTTP Method and Url
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if cookies != nil {
		for _, c := range cookies {
			// Set cookie
			req.AddCookie(&c)
		}
	}
	return client.Do(req)
}

func closeResponseBody(r *http.Response) {
	if err := r.Body.Close(); err != nil {
		glog.Error(err)
	}
}

func (t *testConfig) Setup() error {
	t.gateway = "http://" + tc.Kube.Ingress
	//generate rule yaml files, replace with actual namespace and user
	for _, rule := range []string{allRule, delayRule, fiftyRule, testRule, testDbRule} {
		src := util.GetResourcePath(filepath.Join(rulesDir, rule))
		dest := filepath.Join(t.rulesDir, rule)
		ori, err := ioutil.ReadFile(src)
		if err != nil {
			glog.Errorf("Failed to read original rule file %s", src)
			return err
		}
		content := string(ori)
		content = strings.Replace(content, "default", t.Kube.Namespace, -1)
		content = strings.Replace(content, "jason", u2, -1)
		err = ioutil.WriteFile(dest, []byte(content), 0600)
		if err != nil {
			glog.Errorf("Failed to write into new rule file %s", dest)
			return err
		}

	}
	return setUpDefaultRouting()
}

func (t *testConfig) Teardown() error {
	if err := deleteRules(defaultRules); err != nil {
		// don't report errors if the rule being deleted doesn't exist
		if notFound := strings.Contains(err.Error(), "not found"); notFound {
			return nil
		}
		return err
	}
	return nil
}

func check(err error, msg string) {
	if err != nil {
		glog.Fatalf("%s. Error %s", msg, err)
	}
}

func inspect(err error, fMsg, sMsg string, t *testing.T) {
	if err != nil {
		glog.Errorf("%s. Error %s", fMsg, err)
		t.Error(err)
	} else if sMsg != "" {
		glog.Info(sMsg)
	}
}

func setUpDefaultRouting() error {
	if err := applyRules(defaultRules, create); err != nil {
		return fmt.Errorf("could not apply rule '%s': %v", allRule, err)
	}
	standby := 0
	for i := 0; i <= 10; i++ {
		time.Sleep(time.Duration(standby) * time.Second)
		resp, err := http.Get(fmt.Sprintf("%s/productpage", tc.gateway))
		if err != nil {
			glog.Infof("Error talking to productpage: %s", err)
		} else {
			glog.Infof("Get from page: %d", resp.StatusCode)
			if resp.StatusCode == http.StatusOK {
				glog.Info("Get response from product page!")
				break
			}
			closeResponseBody(resp)
		}
		if i == 10 {
			return errors.New("unable to set default route")
		}
		standby += 5
		glog.Errorf("Couldn't get to the bookinfo product page, trying again in %d second", standby)
	}
	glog.Info("Success! Default route got expected response")
	return nil
}

func checkRoutingResponse(user, version, gateway, modelFile string) (int, error) {
	startT := time.Now()
	cookies := []http.Cookie{
		{
			Name:  "foo",
			Value: "bar",
		},
		{
			Name:  "user",
			Value: user,
		},
	}
	resp, err := getWithCookie(fmt.Sprintf("%s/productpage", gateway), cookies)
	if err != nil {
		return -1, err
	}
	if resp.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("status code is %d", resp.StatusCode)
	}
	duration := int(time.Since(startT) / (time.Second / time.Nanosecond))
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return -1, err
	}

	if err = util.CompareToFile(body, modelFile); err != nil {
		glog.Errorf("Error: User %s in version %s didn't get expected response", user, version)
		duration = -1
	}
	closeResponseBody(resp)
	return duration, err
}

func checkHttpResponse(user, gateway, expr string, count int) (int, error) {
	resp, err := http.Get(fmt.Sprintf("%s/productpage", tc.gateway))
	if err != nil {
		return -1, err
	} else {
		defer closeResponseBody(resp)
		glog.Infof("Get from page: %d", resp.StatusCode)
		if resp.StatusCode != http.StatusOK {
			glog.Errorf("Get response from product page failed!")
			return -1, fmt.Errorf("status code is %d", resp.StatusCode)
		}
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return -1, err
	}

	if expr == "" {
		return 1, nil
	}

	re, err := regexp.Compile(expr)
	if err != nil {
		return -1, err
	}

	ref := re.FindAll(body, -1)
	if ref == nil {
		glog.Infof("%v", string(body))
		return -1, fmt.Errorf("Could not find %v in response", expr)
	}
	if count > 0 && len(ref) < count {
		glog.Infof("%v", string(body))
		return -1, fmt.Errorf("Could not find %v # of %v in response. found %v", count, expr, len(ref))
	}
	return 1, nil
}

func deleteRules(ruleKeys []string) error {
	var err error
	for _, ruleKey := range ruleKeys {
		rule := filepath.Join(tc.rulesDir, ruleKey)
		if e := tc.Kube.Istioctl.DeleteRule(rule); e != nil {
			err = multierror.Append(err, e)
		}
	}
	glog.Info("Waiting for rule to be cleaned up...")
	time.Sleep(time.Duration(30) * time.Second)
	return err
}

func applyRules(ruleKeys []string, operation string) error {
	var fn func(string) error
	switch operation {
	case "create":
		fn = tc.Kube.Istioctl.CreateRule
	case "replace":
		fn = tc.Kube.Istioctl.ReplaceRule
	default:
		return fmt.Errorf("operation %s not supported", operation)
	}
	for _, ruleKey := range ruleKeys {
		rule := filepath.Join(tc.rulesDir, ruleKey)
		if err := fn(rule); err != nil {
			return err
		}
	}
	glog.Info("Waiting for rules to propagate...")
	time.Sleep(time.Duration(30) * time.Second)
	return nil
}

func TestVersionRouting(t *testing.T) {
	var err error
	var rules = []string{testRule}
	inspect(applyRules(rules, create), "failed to apply rules", "", t)
	defer func() {
		inspect(deleteRules(rules), "failed to delete rules", "", t)
	}()

	v1File := util.GetResourcePath(filepath.Join(modelDir, "productpage-normal-user-v1.html"))
	v2File := util.GetResourcePath(filepath.Join(modelDir, "productpage-test-user-v2.html"))

	_, err = checkRoutingResponse(u1, "v1", tc.gateway, v1File)
	inspect(
		err, fmt.Sprintf("Failed version routing! %s in v1", u1),
		fmt.Sprintf("Success! Response matches with expected! %s in v1", u1), t)
	_, err = checkRoutingResponse(u2, "v2", tc.gateway, v2File)
	inspect(
		err, fmt.Sprintf("Failed version routing! %s in v2", u2),
		fmt.Sprintf("Success! Response matches with expected! %s in v2", u2), t)
}

func TestFaultDelay(t *testing.T) {
	var rules = []string{testRule, delayRule}
	inspect(applyRules(rules, create), "failed to apply rules", "", t)
	defer func() {
		inspect(deleteRules(rules), "failed to delete rules", "", t)
	}()
	minDuration := 5
	maxDuration := 8
	standby := 10
	testModel := util.GetResourcePath(
		filepath.Join(modelDir, "productpage-test-user-v1-review-timeout.html"))
	for i := 0; i < testRetryTimes; i++ {
		duration, err := checkRoutingResponse(
			u2, "v1-timeout", tc.gateway,
			testModel)
		glog.Infof("Get response in %d second", duration)
		if err == nil && duration >= minDuration && duration <= maxDuration {
			glog.Info("Success! Fault delay as expected")
			break
		}

		if i == 4 {
			t.Errorf("Fault delay failed! Delay in %ds while expected between %ds and %ds, %s",
				duration, minDuration, maxDuration, err)
			break
		}

		glog.Infof("Unexpected response, retry in %ds", standby)
		time.Sleep(time.Duration(standby) * time.Second)
	}
}

func TestVersionMigration(t *testing.T) {
	var rules = []string{fiftyRule}
	inspect(applyRules(rules, "replace"), "failed to apply rules", "", t)
	defer func() {
		inspect(deleteRules(rules), fmt.Sprintf("failed to delete rules"), "", t)
	}()

	// Percentage moved to new version
	migrationRate := 0.5
	tolerance := 0.05
	totalShot := 100
	modelV1 := util.GetResourcePath(filepath.Join(modelDir, "productpage-normal-user-v1.html"))
	modelV3 := util.GetResourcePath(filepath.Join(modelDir, "productpage-normal-user-v3.html"))

	cookies := []http.Cookie{
		{
			Name:  "foo",
			Value: "bar",
		},
		{
			Name:  "user",
			Value: "normal-user",
		},
	}

	for i := 0; i < testRetryTimes; i++ {
		c1, c3 := 0, 0
		for c := 0; c < totalShot; c++ {
			resp, err := getWithCookie(fmt.Sprintf("%s/productpage", tc.gateway), cookies)
			inspect(err, "Failed to record", "", t)
			if resp.StatusCode != http.StatusOK {
				t.Errorf("unexpected response status %d", resp.StatusCode)
				continue
			}
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Error(err)
				continue
			}
			if err = util.CompareToFile(body, modelV1); err == nil {
				c1++
			} else if err = util.CompareToFile(body, modelV3); err == nil {
				c3++
			}
			closeResponseBody(resp)
		}
		c1Percent := int((migrationRate + tolerance) * float64(totalShot))
		c3Percent := int((migrationRate - tolerance) * float64(totalShot))
		if (c1 <= c1Percent) && (c3 >= c3Percent) {
			glog.Infof(
				"Success! Version migration acts as expected, "+
					"old version hit %d, new version hit %d", c1, c3)
			break
		}

		if i == 4 {
			t.Errorf("Failed version migration test, "+
				"old version hit %d, new version hit %d", c1, c3)
		}
	}
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("demo_test")
	if err != nil {
		return err
	}
	tc = new(testConfig)
	tc.CommonConfig = cc
	tc.rulesDir, err = ioutil.TempDir(os.TempDir(), "demo_test")
	if err != nil {
		return err
	}
	demoApp := &framework.App{
		AppYaml:    util.GetResourcePath(bookinfoYaml),
		KubeInject: true,
	}
	tc.Kube.AppManager.AddApp(demoApp)
	return nil
}

func TestDbRouting(t *testing.T) {
	var err error
	var rules = []string{allRule, testDbRule}
	inspect(applyRules(rules, create), "failed to apply rules", "", t)
	defer func() {
		inspect(deleteRules(rules), "failed to delete rules", "", t)
	}()

	var respExpr string = "glyphicon-star"

	_, err = checkHttpResponse(u1, tc.gateway, respExpr, 10)
	inspect(
		err, fmt.Sprintf("Failed database routing! %s in v1", u1),
		fmt.Sprintf("Success! Response matches with expected! %s", respExpr), t)
}

func TestMain(m *testing.M) {
	flag.Parse()
	check(framework.InitGlog(), "cannot setup glog")
	check(setTestConfig(), "could not create TestConfig")
	tc.Cleanup.RegisterCleanable(tc)
	os.Exit(tc.RunTest(m))
}
