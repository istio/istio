package e2e

import (
	"flag"
	"github.com/golang/glog"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/e2e/util"
	"os"
	"testing"
	"time"
	"fmt"

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
	if err := c.Kube.DeployAppFromYaml("demos/apps/bookinfo/bookinfo.yaml", true); err != nil {
		return err
	}

	// Option2 : deploy app from template
	/*
	if err := c.Kube.DeployAppFromTmpl("t", "t", "8080", "80", "9090", "90", "unversioned", true); err != nil {
		return err
	}
*/
	return nil
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
	t.Logf("Value is %s", c.sampleValue)
}

func TestDefaultRoute(t *testing.T) {
	glog.Info("Start default route test.")
	gateway := "http://" + c.Kube.Ingress
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
				break;
			}
		}
		if i == 5 {
			t.Error("Default route Failed")
			return
		}
		standby += 5
		glog.Info("Couldn't get to the bookinfo product page, trying again in %d second", standby)
	}
	glog.Info("Default route succeed!")
}

func NewTestConfig() (*testConfig, error) {
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
	framework.InitGlog()
	var err error
	c, err = NewTestConfig()
	if err != nil {
		glog.Fatalf("Could not create TestConfig %s", err)
	}
	c.Cleanup.RegisterCleanable(c)
	os.Exit(c.RunTest(m))
}
