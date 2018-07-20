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

package e2e

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	u1              = "normal-user"
	u2              = "test-user"
	bookinfoYaml    = "samples/bookinfo/platform/kube/bookinfo.yaml"
	bookinfoGateway = "bookinfo-gateway.yaml"
	modelDir        = "tests/apps/bookinfo/output"
	rulesDir        = "samples/bookinfo/networking"
	allRule         = "virtual-service-all-v1.yaml"
	testRule        = "virtual-service-reviews-test-v2.yaml"
	testRetryTimes  = 10
)

var (
	tc                *testConfig
	baseConfig        *framework.CommonConfig
	targetConfig      *framework.CommonConfig
	defaultRules      = []string{allRule, testRule, bookinfoGateway}
	flagBaseVersion   = flag.String("base_version", "0.4.0", "Base version to upgrade from.")
	flagTargetVersion = flag.String("target_version", "0.5.1", "Target version to upgrade to.")
	flagSmoothCheck   = flag.Bool("smooth_check", false, "Whether to check the upgrade is smooth.")
)

type testConfig struct {
	*framework.CommonConfig
	gateway  string
	rulesDir string
}

func (t *testConfig) Setup() error {
	//generate rule yaml files, replace "jason" with actual user
	for _, rule := range defaultRules {
		src := util.GetResourcePath(filepath.Join(rulesDir, rule))
		dest := filepath.Join(t.rulesDir, rule)
		ori, err := ioutil.ReadFile(src)
		if err != nil {
			log.Errorf("Failed to read original rule file %s", src)
			return err
		}
		content := string(ori)
		content = strings.Replace(content, "jason", u2, -1)
		err = ioutil.WriteFile(dest, []byte(content), 0600)
		if err != nil {
			log.Errorf("Failed to write into new rule file %s", dest)
			return err
		}

	}

	if !util.CheckPodsRunning(tc.Kube.Namespace, tc.Kube.KubeConfig) {
		return fmt.Errorf("can't get all pods running")
	}

	gateway, errGw := tc.Kube.Ingress()
	if errGw != nil {
		return errGw
	}

	t.gateway = gateway

	return setUpDefaultRouting()
}
func getWithCookie(url string, cookies []http.Cookie) (*http.Response, error) {
	// Declare http client
	client := &http.Client{}

	// Declare HTTP Method and Url
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for _, c := range cookies {
		// Set cookie
		req.AddCookie(&c)
	}
	return client.Do(req)
}

func closeResponseBody(r *http.Response) {
	if err := r.Body.Close(); err != nil {
		log.Errora(err)
	}
}

func (t *testConfig) Teardown() error {
	if err := deleteRules(defaultRules); err != nil {
		// don't report errors if the rule being deleted doesn't exist
		if notFound := strings.Contains(err.Error(), "not found"); notFound {
			return nil
		}
		return err
	}
	return nil
}

func check(err error, msg string) {
	if err != nil {
		log.Errorf("%s. Error %s", msg, err)
		os.Exit(-1)
	}
}

func inspect(err error, fMsg, sMsg string, t *testing.T) {
	if err != nil {
		log.Errorf("%s. Error %s", fMsg, err)
		t.Error(err)
	} else if sMsg != "" {
		log.Info(sMsg)
	}
}

func probeGateway(retryTimes int) error {
	var err1, err2 error
	standby := 0
	v1File := util.GetResourcePath(filepath.Join(modelDir, "productpage-normal-user-v1.html"))
	v2File := util.GetResourcePath(filepath.Join(modelDir, "productpage-test-user-v2.html"))
	for i := 0; i <= retryTimes; i++ {
		time.Sleep(time.Duration(standby) * time.Second)
		_, err1 = checkRoutingResponse(u1, "v1", tc.gateway, v1File)
		_, err2 = checkRoutingResponse(u2, "v2", tc.gateway, v2File)
		if err1 == nil && err2 == nil {
			log.Infof("Successfully getting response from gateway.")
			return nil
		}
		standby += 5
		log.Warnf("Couldn't get to the bookinfo product page, trying again in %d second", standby)
	}
	if err1 != nil {
		log.Errorf("Failed version routing! %s in v1: %s", u1, err1)
	}
	if err2 != nil {
		log.Errorf("Failed version routing! %s in v2: %s", u2, err2)
	}
	return errors.New("unable to get valid response from gateway")
}

func setUpDefaultRouting() error {
	if err := applyRules(defaultRules); err != nil {
		return fmt.Errorf("could not apply rule '%s': %v", allRule, err)
	}
	return probeGateway(testRetryTimes)
}

func checkRoutingResponse(user, version, gateway, modelFile string) (int, error) {
	startT := time.Now()
	cookies := []http.Cookie{
		{
			Name:  "foo",
			Value: "bar",
		},
		{
			Name:  "user",
			Value: user,
		},
	}
	resp, err := getWithCookie(fmt.Sprintf("%s/productpage", gateway), cookies)
	if err != nil {
		return -1, err
	}
	if resp.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("status code is %d", resp.StatusCode)
	}
	duration := int(time.Since(startT) / (time.Second / time.Nanosecond))
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return -1, err
	}

	if err = util.CompareToFile(body, modelFile); err != nil {
		duration = -1
	}
	closeResponseBody(resp)
	return duration, err
}

func deleteRules(ruleKeys []string) error {
	var err error
	for _, ruleKey := range ruleKeys {
		rule := filepath.Join(tc.rulesDir, ruleKey)
		if e := util.KubeDelete(tc.Kube.Namespace, rule, tc.Kube.KubeConfig); e != nil {
			err = multierror.Append(err, e)
		}
	}
	log.Info("Waiting for rule to be cleaned up...")
	time.Sleep(time.Duration(30) * time.Second)
	return err
}

func applyRules(ruleKeys []string) error {
	for _, ruleKey := range ruleKeys {
		rule := filepath.Join(tc.rulesDir, ruleKey)
		if err := util.KubeApply(tc.Kube.Namespace, rule, tc.Kube.KubeConfig); err != nil {
			//log.Errorf("Kubectl apply %s failed", rule)
			return err
		}
	}
	log.Info("Waiting for rules to propagate...")
	time.Sleep(time.Duration(30) * time.Second)
	return nil
}

func pruneDeprecatedResources() {
	if err := util.DeleteDeployment("istio-ca", tc.Kube.Namespace, tc.Kube.KubeConfig); err != nil {
		log.Warnf("Delete deployment istio-ca failed %q", err)
	}
	if err := util.DeleteDeployment("istio-mixer", tc.Kube.Namespace, tc.Kube.KubeConfig); err != nil {
		log.Warnf("Delete deployment istio-mixer failed %q", err)
	}
}

func upgradeControlPlane() error {
	if baseConfig.Kube.BaseVersion <= "0.7.1" {
		pruneDeprecatedResources()
	}
	// Generate and deploy Isito yaml files.
	err := targetConfig.Kube.Setup()
	if err != nil {
		return err
	}
	if !util.CheckPodsRunningWithMaxDuration(targetConfig.Kube.Namespace, 600*time.Second, tc.Kube.KubeConfig) {
		return fmt.Errorf("can't get all pods running when upgrading control plane")
	}
	if _, err = util.Shell("kubectl get all -n %s -o wide", targetConfig.Kube.Namespace); err != nil {
		return err
	}
	// TODO: Check control plane version.
	// Update gateway address
	gateway, errGw := targetConfig.Kube.Ingress()
	if errGw != nil {
		return errGw
	}

	tc.gateway = gateway
	return nil
}

func upgradeSidecars() error {
	err := targetConfig.Kube.Istioctl.Setup()
	if err != nil {
		return err
	}
	err = targetConfig.Kube.AppManager.Setup()
	if err != nil {
		return err
	}
	if !util.CheckPodsRunningWithMaxDuration(targetConfig.Kube.Namespace, 600*time.Second, tc.Kube.KubeConfig) {
		return fmt.Errorf("can't get all pods running when upgrading sidecar")
	}
	// TODO: Check sidecar version.
	return nil
}

func TestUpgrade(t *testing.T) {
	t.Skip("https://github.com/istio/istio/issues/4937")

	err := upgradeControlPlane()
	inspect(err, "Failed to upgrade control plane", "Control plane upgraded.", t)
	if err != nil {
		return
	}
	if *flagSmoothCheck {
		err = probeGateway(testRetryTimes)
		inspect(err, "Probing Gateway failed after control plane upgraded.", "", t)
	}
	err = upgradeSidecars()
	inspect(err, "Failed to upgrade sidecars.", "Sidecar upgraded.", t)
	if err != nil {
		return
	}
	err = probeGateway(testRetryTimes)
	inspect(err, "Probing Gateway failed after sidecar upgraded.", "", t)
}

func setTestConfig() error {
	var err error
	baseConfig, err = framework.NewCommonConfigWithVersion("upgrade_test", *flagBaseVersion)
	if err != nil {
		return err
	}
	targetConfig, err = framework.NewCommonConfigWithVersion("upgrade_test", *flagTargetVersion)
	if err != nil {
		return err
	}
	demoApps := []framework.App{
		{
			AppYaml:    util.GetResourcePath(bookinfoYaml),
			KubeInject: true,
		},
	}
	for i := range demoApps {
		baseConfig.Kube.AppManager.AddApp(&demoApps[i])
		targetConfig.Kube.AppManager.AddApp(&demoApps[i])
	}
	tc = new(testConfig)
	tc.CommonConfig = baseConfig
	tc.rulesDir, err = ioutil.TempDir(os.TempDir(), "upgrade_test")
	return err
}

func TestMain(m *testing.M) {
	// https://github.com/istio/istio/issues/4937
	// flag.Parse()
	// check(framework.InitLogging(), "cannot setup logging")
	// check(setTestConfig(), "could not create TestConfig")
	// tc.Cleanup.RegisterCleanable(tc)
	// os.Exit(tc.RunTest(m))
}
