// Copyright 2017 Istio Authors
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

/*
Simple test - first time:
source istio.VERSION
bazel run //tests/e2e/tests/simple:go_default_test -- -alsologtostderr -test.v -v 2 \
    -test.run TestSimple1 --skip_cleanup --auth_enable --namespace=e2e
After which to Retest:
bazel run //tests/e2e/tests/simple:go_default_test -- -alsologtostderr -test.v -v 2 \
    -test.run TestSimple1 --skip_setup --skip_cleanup --auth_enable --namespace=e2e
*/

package simple

import (
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang/glog"

	"istio.io/istio/devel/fortio"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/e2e/util"
)

const (
	servicesYaml    = "tests/e2e/tests/simple/servicesToBeInjected.yaml"
	nonInjectedYaml = "tests/e2e/tests/simple/servicesNotInjected.yaml"
)

type testConfig struct {
	*framework.CommonConfig
}

var (
	tc *testConfig
)

func TestMain(m *testing.M) {
	flag.Parse()
	check(framework.InitGlog(), "cannot setup glog")
	check(setTestConfig(), "could not create TestConfig")
	os.Exit(tc.RunTest(m))
}

func TestSimple1(t *testing.T) {
	//url := "http://" + tc.Kube.Ingress + "/fortio/debug" // not working but should
	url := "http://" + tc.Kube.Ingress + "/debug" // works through direct mapping
	glog.Infof("Fetching '%s'", url)
	attempts := 5 // if it takes more than 50s to be live...
	for i := 1; i <= attempts; i++ {
		if i > 1 {
			time.Sleep(10 * time.Second) // wait between retries
		}
		resp, err := http.Get(url)
		if err != nil {
			glog.Warningf("Attempt %d : http.Get error %v", i, err)
			continue
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			glog.Warningf("Attempt %d : ReadAll error %v", i, err)
			continue
		}
		_ = resp.Body.Close()
		bodyStr := string(body)
		glog.Infof("Attempt %d: reply is\n%s\n---END--", i, bodyStr)
		needle := "echo debug server on echosrv"
		if !strings.Contains(bodyStr, needle) {
			glog.Warningf("Not finding expected %s in %s", needle, fortio.DebugSummary(body, 128))
			continue
		}
		return // success
	}
	t.Errorf("Unable to find expected output after %d attempts", attempts)
}

type fortioTemplate struct {
	FortioImage string
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("simple_auth_test")
	if err != nil {
		return err
	}
	tc = new(testConfig)
	tc.CommonConfig = cc
	hub := os.Getenv("FORTIO_HUB")
	tag := os.Getenv("FORTIO_TAG")
	image := hub + "/fortio:" + tag
	if hub == "" || tag == "" {
		image = "ldemailly/fortio:latest" // TODO: change
	}
	glog.Infof("Fortio hub %s tag %s -> image %s", hub, tag, image)
	services := []framework.App{
		{
			KubeInject:      true,
			AppYamlTemplate: util.GetResourcePath(servicesYaml),
			Template: &fortioTemplate{
				FortioImage: image,
			},
		},
		{
			KubeInject:      false,
			AppYamlTemplate: util.GetResourcePath(nonInjectedYaml),
			Template: &fortioTemplate{
				FortioImage: image,
			},
		},
	}
	for i := range services {
		tc.Kube.AppManager.AddApp(&services[i])
	}
	return nil
}

func check(err error, msg string) {
	if err != nil {
		glog.Fatalf("%s. Error %s", msg, err)
	}
}
