package e2e

import (
	"flag"
	"github.com/golang/glog"
	"istio.io/istio/tests/e2e/framework"
	"os"
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
	if err := framework.DeployApp(c.Kube, "t", "t", "8080", "80", "9090", "90", "unversioned", false); err != nil {
		return err
	}
	if err := framework.DeployApp(c.Kube, "a", "a", "8080", "80", "9090", "90", "v1", true); err != nil {
		return err
	}
	if err := framework.DeployApp(c.Kube, "b", "b", "80", "8080", "90", "9090", "unversioned", true); err != nil {
		return err
	}

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

func NewTestConfig() *testConfig {
	return &testConfig{
		CommonConfig: framework.NewCommonConfig("sample-test"),
	}
}

func TestMain(m *testing.M) {
	flag.Parse()
	c = NewTestConfig()
	c.CommonConfig.Cleanup.RegisterCleanable(c)
	os.Exit(c.RunTest(m))
}
