package simple

import (
	"flag"
	"os"
	"strings"
	"testing"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	timeToWaitForPods    = 20 * time.Second
	timeToWaitForIngress = 100 * time.Second
)

type testConfig struct {
	*framework.CommonConfig
}

var (
	tc        *testConfig
	testFlags = &framework.TestFlags{
		Ingress: true,
		Egress:  true,
	}
)

func init() {
	testFlags.Init()
	flag.Parse()
}

func TestMain(m *testing.M) {
	if err := framework.InitLogging(); err != nil {
		panic("cannot setup logging")
	}
	if err := setTestConfig(); err != nil {
		log.Error("could not create TestConfig")
		os.Exit(-1)
	}
	os.Exit(tc.RunTest(m))
}

func TestKialiPod(t *testing.T) {
	ns := tc.Kube.Namespace
	// Get the 2 pods
	podList, err := getPodList(ns, "app=kiali")
	if err != nil {
		t.Fatalf("kubectl failure to get pods %v", err)
	}
	if len(podList) != 1 {
		t.Fatalf("Unexpected to get %d pods when expecting 1. got %v", len(podList), podList)
	}
}

func getPodList(namespace string, selector string) ([]string, error) {
	pods, err := util.Shell("kubectl get pods -n %s -l %s -o jsonpath={.items[*].metadata.name}", namespace, selector)
	if err != nil {
		return nil, err
	}
	return strings.Split(pods, " "), nil
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("kiali_test")
	if err != nil {
		return err
	}
	tc = new(testConfig)
	tc = &testConfig{CommonConfig: cc}
	return nil
}
