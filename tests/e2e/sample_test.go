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

var (
	c *testConfig
)

type testConfig struct {
	*framework.CommonConfig
	sampleValue string
}

func (c *testConfig) deployApps() error {
	// Option1 : deploy app directly from yaml
	err := c.Kube.DeployAppFromYaml("demos/apps/bookinfo/bookinfo.yaml", true)
	return err

	// Option2 : deploy app from template
	/*
		err := c.Kube.DeployAppFromTmpl("t", "t", "8080", "80", "9090", "90", "unversioned", true)
		return err
	*/
}

func check(err error, msg string) {
	if err != nil {
		glog.Fatalf("%s. Error %s", msg, err)
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

	t.Logf("Value is %s", c.sampleValue)
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
		glog.Infof("Couldn't get to the bookinfo product page, trying again in %d second", standby)
	}
	glog.Info("Default route succeed!")
}

func checkRoutingOutput(user, version, gateway string) error {
	outputFile := fmt.Sprintf("productpage-%s-%s.html", user, version)
	output := filepath.Join(c.Kube.TmpDir, outputFile)
	resp, err := util.Shell(fmt.Sprintf("curl -s -b 'foo=bar;user=%s;' %s/productpage",
		user, gateway))
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(output, []byte(resp), 0600); err != nil {
		return err
	}
	if err := util.CompareFiles(output, util.GetTestRuntimePath(filepath.Join("tests/apps/bookinfo/output", outputFile))); err != nil {
		glog.Errorf("Failed! User %s in version %s didn't get expected respose", user, version)
		return err
	} else {
		glog.Infof("Succeed! User %s in version %s get expected respose!", user, version)
		return nil
	}
}

func prepareRule(src, user string) (string, error) {
	dst := filepath.Join(filepath.Join(c.Kube.TmpDir, "yaml"), path.Base(src))
	ori, err := ioutil.ReadFile(src)
	if err != nil {
		glog.Errorf("Cannot read original rule file %s", src)
		return "", err
	}
	content := string(ori)
	content = strings.Replace(content, "default", c.Kube.Namespace, -1)
	content = strings.Replace(content, "jason", user, -1)
	err = ioutil.WriteFile(dst, []byte(content), 0600)
	if err != nil {
		glog.Errorf("Cannot write into new rule file %s", dst)
		return "", err
	}
	return dst, nil
}

func testVersionRouting(t *testing.T, gateway string) {
	var r1, r2 string
	var err error
	u1 := "normal-user"
	u2 := "test-user"
	util.PrintBlock("Start version routing test...")
	r1, err = prepareRule(util.GetTestRuntimePath("demos/apps/bookinfo/route-rule-all-v1.yaml"), u1)
	check(err, "Failed to prepare rule-all-v1")
	r2, err = prepareRule(util.GetTestRuntimePath("demos/apps/bookinfo/route-rule-reviews-test-v2.yaml"), u2)
	check(err, "Failed to prepare rule-test-v2")
	c.Kube.Istioctl.CreateRule(r1)
	c.Kube.Istioctl.CreateRule(r2)
	glog.Info("Waiting for rules to propagate...")
	time.Sleep(time.Duration(30) * time.Second)

	if err := checkRoutingOutput(u1, "v1", gateway); err != nil {
		glog.Errorf("Unmatch part: %s", err)
		t.Error(err)
	}

	if err := checkRoutingOutput(u2, "v2", gateway); err != nil {
		glog.Errorf("Unmatch part: %s", err)
		t.Error(err)
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
