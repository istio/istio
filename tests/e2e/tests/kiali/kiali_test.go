// Copyright 2019 Istio Authors. All Rights Reserved.
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

package kiali

import (
	"flag"
	"os"
	"testing"

	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
	"istio.io/pkg/log"
)

type testConfig struct {
	*framework.CommonConfig
}

var (
	tc *testConfig
)

func TestMain(m *testing.M) {
	flag.Parse()
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
