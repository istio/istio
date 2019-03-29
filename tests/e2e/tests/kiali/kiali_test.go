package kiali

import (
	"flag"
	"os"
	"testing"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

type testConfig struct {
	*framework.CommonConfig
}

var (
	tc *testConfig
)

func init() {
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
	kubeconfig := tc.Kube.KubeConfig

	// Get the kiali pod
	kialiPod, err := util.GetPodName(ns, "app=kiali", kubeconfig)

	if err != nil {
		t.Fatalf("Kiali Pod was not found: %v", err)
	}

	if status := util.GetPodStatus(ns, kialiPod, kubeconfig); status != "Running" {
		t.Fatalf("Kiali Pod is not Running")
	}
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
