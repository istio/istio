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
	"os"
	"testing"

	"github.com/golang/glog"

	"istio.io/istio/tests/e2e/framework"
)

var (
	c *testConfig
)

type testConfig struct {
	*framework.CommonConfig
	sampleValue string
}

func check(err error, msg string) {
	if err != nil {
		glog.Fatalf("%s. Error %s", msg, err)
	}
}

func (c *testConfig) Setup() error {
	glog.Info("Sample test Setup")
	c.sampleValue = "sampleValue"
	return nil
}

func (c *testConfig) Teardown() error {
	glog.Info("Sample test Tear Down")
	return nil
}

func TestSample(t *testing.T) {
	t.Logf("Value is %s", c.sampleValue)
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
