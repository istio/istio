package e2e

import (
	"flag"
	"github.com/golang/glog"
	"istio.io/istio/tests/e2e/framework"
	"testing"
)

var (
	c *testConfig
)

type testConfig struct {
	*framework.CommonConfig
	sampleValue string
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

func TestSample(t *testing.T) {
	t.Log("Value is ", c.sampleValue)
}

func NewTestConfig() *testConfig{
	return &testConfig{
		CommonConfig: &framework.CommonConfig{
			Info: *framework.NewTestInfo("sample_test"),
		},
	}
}

func TestMain(m *testing.M) {
	flag.Parse()
	c = NewTestConfig()
	framework.E2eTestMain(m, c, c.CommonConfig)
}
