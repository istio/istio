package e2e

import (
	"flag"
	"github.com/golang/glog"
	"istio.io/istio/tests/e2e/framework"
	"testing"
)

type testConfig struct {
	*framework.TestConfig
	sampleValue string
}

func (c *testConfig) SetTestConfig(t *framework.TestConfig) {
	c.TestConfig = t
}

func (c *testConfig) TestId() string {
	return "sample_test"
}

func (c *testConfig) SetUp() error {
	glog.Info("Sample test Setup")
	c.sampleValue = "sampleValue"
	return nil
}

func (c *testConfig) TearDown() error {
	glog.Info("Sample test Tear Down")
	return nil
}

var c = new(testConfig)

func TestSample(t *testing.T) {
	t.Log("Value is ", c.sampleValue)
}

func TestMain(m *testing.M) {
	flag.Parse()
	*c = testConfig{}
	framework.TestMain(m, c)
}
