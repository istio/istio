// Copyright 2017 Google Inc.
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
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/e2e/util"
)

const (
	u1          = "normal-user"
	u2          = "test-user"
	modelDir    = "tests/apps/bookinfo/output"
	bookinfoDir = "demos/apps/bookinfo"
	create      = "create"
	replace     = "replace"
)

var (
	c *testConfig
)

type testConfig struct {
	*framework.CommonConfig
	sampleValue string
}

func (c *testConfig) deployApps() error {
	// Option1 : deploy app directly from yaml
	err := c.Kube.DeployAppFromYaml(filepath.Join(bookinfoDir, "bookinfo.yaml"), true)
	return err

	// Option2 : deploy app from template
	/*
		err = c.Kube.DeployAppFromTmpl("t", "t", "8080", "80", "9090", "90", "unversioned", true)
		return err
	*/
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
		glog.Info(len(sMsg))
		glog.Info(sMsg)
	}
}

func (c *testConfig) Setup() error {
	if err := c.deployApps(); err != nil {
		return err
	}

	glog.Info("Sample test Setup")
	c.sampleValue = "sampleValue"
	return nil
}

func (c *testConfig) Teardown() error {
	glog.Info("Sample test Tear Down")
	return nil
}

func TestSample(t *testing.T) {
	gateway := "http://" + c.Kube.Ingress

	testDefaultRouting(t, gateway)

	testVersionRouting(t, gateway)

	testFaultDelay(t, gateway)

	testVersionMigration(t, gateway)
}

func testDefaultRouting(t *testing.T, gateway string) {
	util.PrintBlock("Start default route test...")
	standby := 0
	for i := 0; i <= 5; i++ {
		time.Sleep(time.Duration(standby) * time.Second)
		resp, err := util.Shell(fmt.Sprintf("curl --write-out %%{http_code} --silent --output /dev/null %s/productpage", gateway))
		if err != nil {
			t.Logf("Error when curl productpage: %s", err)
		} else {
			glog.Infof("Get from page: %s", resp)
			if resp == "200" {
				t.Log("Get response from product page!")
				break
			}
		}
		if i == 5 {
			t.Error("Default route Failed")
			return
		}
		standby += 5
		glog.Infof("\nFailed Couldn't get to the bookinfo product page, trying again in %d second", standby)
	}
	glog.Info("\nSucceed! Default route get expected response")
}

func checkRoutingResponse(user, version, gateway, modelFile string) (int, error) {
	var err error
	outputFile := fmt.Sprintf("productpage-%s-%s.html", user, version)
	output := filepath.Join(c.Kube.TmpDir, outputFile)

	startT := time.Now()
	if err = util.Record(fmt.Sprintf("curl -s -b 'foo=bar;user=%s;' %s/productpage", user, gateway), output); err != nil {
		return -1, err
	}
	duration := int(time.Since(startT) / (time.Second / time.Nanosecond))

	if err = util.CompareFiles(output, util.GetTestRuntimePath(modelFile)); err != nil {
		glog.Errorf("Error: User %s in version %s didn't get expected respose", user, version)
		duration = -1
	}
	return duration, err
}

func applyRule(src, user, operation string) error {
	dst := filepath.Join(c.Kube.TmpDir, "yaml", path.Base(src))
	ori, err := ioutil.ReadFile(src)
	if err != nil {
		glog.Errorf("Cannot read original rule file %s", src)
		return err
	}
	content := string(ori)
	content = strings.Replace(content, "default", c.Kube.Namespace, -1)
	content = strings.Replace(content, "jason", user, -1)
	err = ioutil.WriteFile(dst, []byte(content), 0600)
	if err != nil {
		glog.Errorf("Cannot write into new rule file %s", dst)
		return err
	}

	switch operation {
	case "create":
		err = c.Kube.Istioctl.CreateRule(dst)
	case "replace":
		err = c.Kube.Istioctl.ReplaceRule(dst)
	}
	return err
}

func testVersionRouting(t *testing.T, gateway string) {
	var err error
	util.PrintBlock("Start version routing test")
	inspect(applyRule(util.GetTestRuntimePath(filepath.Join(bookinfoDir, "route-rule-all-v1.yaml")), u1, create), "Failed to apply rule-all-v1", "", t)
	inspect(applyRule(util.GetTestRuntimePath(filepath.Join(bookinfoDir, "route-rule-reviews-test-v2.yaml")), u2, create), "Failed to apply rule-test-v2", "", t)

	glog.Info("Waiting for rules to propagate...")
	time.Sleep(time.Duration(30) * time.Second)

	_, err = checkRoutingResponse(u1, "v1", gateway, filepath.Join(modelDir, "productpage-normal-user-v1.html"))
	inspect(err, fmt.Sprintf("Failed version routing! %s in v1", u1), fmt.Sprintf("\nSucceed! Response matches with expect! %s in v1", u1), t)
	_, err = checkRoutingResponse(u2, "v2", gateway, filepath.Join(modelDir, "productpage-test-user-v2.html"))
	inspect(err, fmt.Sprintf("Failed version routing! %s in v2", u2), fmt.Sprintf("\nSucceed! Response matches with expect! %s in v2", u2), t)
}

func testFaultDelay(t *testing.T, gateway string) {
	util.PrintBlock("Start fault delay test")
	inspect(applyRule(util.GetTestRuntimePath(filepath.Join(bookinfoDir, "route-rule-delay.yaml")), "", create), "Failed to apply rule-delay", "", t)
	minDuration := 5
	maxDuration := 8
	standby := 10
	for i := 0; i < 5; i++ {
		duration, err := checkRoutingResponse(u2, "v1-timeout", gateway, filepath.Join(modelDir, "productpage-test-user-v1-review-timeout.html"))
		glog.Infof("Get response in %d second", duration)
		if err == nil && duration >= minDuration && duration <= maxDuration {
			glog.Info("\nSucceed! Fault delay as expecetd")
			break
		}

		if i == 4 {
			t.Errorf("Fault delay failed! Delay in %ds while expected between %ds and %ds, %s", duration, minDuration, maxDuration, err)
			break
		}

		glog.Infof("Unexpected response, retry in %ds", standby)
		time.Sleep(time.Duration(standby) * time.Second)
	}

	inspect(c.Kube.Istioctl.DeleteRule(filepath.Join(c.Kube.TmpDir, "yaml", "route-rule-delay.yaml")), "Error when deleting delay rule", "", t)
	glog.Info("Waiting for delay rule to be cleaned...")
	time.Sleep(time.Duration(30) * time.Second)
}

func testVersionMigration(t *testing.T, gateway string) {
	util.PrintBlock("Start version migration test")
	inspect(applyRule(util.GetTestRuntimePath(filepath.Join(bookinfoDir, "route-rule-reviews-50-v3.yaml")), "normal-user", replace),
		"Failed to apply 50 rule", "", t)
	glog.Info("Waiting for rules to propagate...")
	time.Sleep(time.Duration(30) * time.Second)

	// Percentage moved to new version
	migrationRate := 0.5
	tolerance := 0.05
	totalShot := 100
	output := filepath.Join(c.Kube.TmpDir, "version-migration.html")
	for i := 0; i < 5; i++ {
		c1, c3 := 0, 0
		for c := 0; c < totalShot; c++ {
			inspect(util.Record(fmt.Sprintf("curl -s -b 'foo=bar;user=normal-user;' %s/productpage", gateway), output), "Failed to record", "", t)
			if err := util.CompareFiles(output, util.GetTestRuntimePath(filepath.Join(modelDir, "productpage-normal-user-v1.html"))); err == nil {
				c1++
			} else if err := util.CompareFiles(output, util.GetTestRuntimePath(filepath.Join(modelDir, "productpage-normal-user-v3.html"))); err == nil {
				c3++
			}
		}

		if (c1 <= int((migrationRate+tolerance)*float64(totalShot))) && (c3 >= int((migrationRate-tolerance)*float64(totalShot))) {
			glog.Infof("\nSucceed! Version migration acts as expected, old version hit %d, new version hit %d", c1, c3)
			break
		}

		if i == 4 {
			t.Errorf("Failed version migration test, old version hit %d, new version hit %d", c1, c3)
		}
	}

}

func newTestConfig() (*testConfig, error) {
	cc, err := framework.NewCommonConfig("sample_test")
	if err != nil {
		return nil, err
	}
	t := new(testConfig)
	t.CommonConfig = cc
	return t, nil
}

func TestMain(m *testing.M) {
	flag.Parse()
	check(framework.InitGlog(), "Cannot setup glog")
	var err error
	c, err = newTestConfig()
	check(err, "Could not create TestConfig")
	c.Cleanup.RegisterCleanable(c)
	os.Exit(c.RunTest(m))
}
