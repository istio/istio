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
	"regexp"
	"strings"
	"testing"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

const (
	u1                                 = "normal-user"
	u2                                 = "test-user"
	bookinfoSampleDir                  = "samples/bookinfo"
	yamlExtension                      = "yaml"
	deploymentDir                      = "platform/kube"
	routeRulesDir                      = "networking"
	bookinfoYaml                       = "bookinfo"
	bookinfoRatingsv2Yaml              = "bookinfo-ratings-v2"
	bookinfoRatingsMysqlYaml           = "bookinfo-ratings-v2-mysql"
	bookinfoDbYaml                     = "bookinfo-db"
	bookinfoMysqlYaml                  = "bookinfo-mysql"
	bookinfoDetailsExternalServiceYaml = "bookinfo-details-v2"
	modelDir                           = "tests/apps/bookinfo/output"
	bookinfoGateway                    = routeRulesDir + "/" + "bookinfo-gateway"
	destRule                           = routeRulesDir + "/" + "destination-rule-all"
	destRuleMtls                       = routeRulesDir + "/" + "destination-rule-all-mtls"
	allRule                            = routeRulesDir + "/" + "virtual-service-all-v1"
	delayRule                          = routeRulesDir + "/" + "virtual-service-ratings-test-delay"
	tenRule                            = routeRulesDir + "/" + "virtual-service-reviews-90-10"
	twentyRule                         = routeRulesDir + "/" + "virtual-service-reviews-80-20"
	fiftyRule                          = routeRulesDir + "/" + "virtual-service-reviews-50-v3"
	testRule                           = routeRulesDir + "/" + "virtual-service-reviews-test-v2"
	testDbRule                         = routeRulesDir + "/" + "virtual-service-ratings-db"
	testMysqlRule                      = routeRulesDir + "/" + "virtual-service-ratings-mysql"
	detailsExternalServiceRouteRule    = routeRulesDir + "/" + "virtual-service-details-v2"
	detailsExternalServiceEgressRule   = routeRulesDir + "/" + "egress-rule-google-apis"
	reviewsDestinationRule             = routeRulesDir + "/" + "destination-policy-reviews"
)

var (
	tc *testConfig
	tf = &framework.TestFlags{
		Ingress: true,
		Egress:  true,
	}
	testRetryTimes = 5
	defaultRules   = []string{bookinfoGateway}
	allRules       = []string{delayRule, tenRule, twentyRule, fiftyRule, testRule,
		testDbRule, testMysqlRule, detailsExternalServiceRouteRule,
		detailsExternalServiceEgressRule, bookinfoGateway}
)

type testConfig struct {
	*framework.CommonConfig
	rulesDir string
}

func init() {
	tf.Init()
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

func getPreprocessedRulePath(t *testConfig, version, rule string) string {
	// transform, for example "routing/virtual-service" into
	// "{t.rulesDir}/routing/v1aplha3/virtual-service.yaml"
	parts := strings.Split(rule, string(os.PathSeparator))
	parts[len(parts)-1] = parts[len(parts)-1] + "." + yamlExtension

	return util.GetResourcePath(filepath.Join(t.rulesDir, routeRulesDir, version,
		strings.Join(parts[1:], string(os.PathSeparator))))
}

func getOriginalRulePath(version, rule string) string {
	parts := strings.Split(rule, string(os.PathSeparator))

	parts[0] = routeRulesDir
	parts[len(parts)-1] = parts[len(parts)-1] + "." + yamlExtension
	return util.GetResourcePath(filepath.Join(bookinfoSampleDir,
		strings.Join(parts, string(os.PathSeparator))))
}

func preprocessRule(t *testConfig, version, rule string) error {
	src := getOriginalRulePath(version, rule)
	dest := getPreprocessedRulePath(t, version, rule)
	ori, err := ioutil.ReadFile(src)
	if err != nil {
		log.Errorf("Failed to read original rule file %s", src)
		return err
	}
	content := string(ori)
	content = strings.Replace(content, "jason", u2, -1)

	err = os.MkdirAll(filepath.Dir(dest), 0700)
	if err != nil {
		log.Errorf("Failed to create the directory %s", filepath.Dir(dest))
		return err
	}

	err = ioutil.WriteFile(dest, []byte(content), 0600)
	if err != nil {
		log.Errorf("Failed to write into new rule file %s", dest)
		return err
	}

	return nil
}

func (t *testConfig) Setup() error {
	//generate rule yaml files, replace "jason" with actual user
	if tc.Kube.AuthEnabled {
		allRules = append(allRules, routeRulesDir+"/"+"destination-rule-all-mtls")
		defaultRules = append(defaultRules, routeRulesDir+"/"+"destination-rule-all-mtls")
	} else {
		allRules = append(allRules, routeRulesDir+"/"+"destination-rule-all")
		defaultRules = append(defaultRules, routeRulesDir+"/"+"destination-rule-all")
	}
	allRules = append(allRules, routeRulesDir+"/"+"virtual-service-all-v1")
	defaultRules = append(defaultRules, routeRulesDir+"/"+"virtual-service-all-v1")
	for _, rule := range allRules {
		err := preprocessRule(t, "v1alpha3", rule)
		if err != nil {
			return nil
		}
	}

	if !util.CheckPodsRunning(tc.Kube.Namespace, tc.Kube.KubeConfig) {
		return fmt.Errorf("can't get all pods running")
	}

	return setUpDefaultRouting()
}

func (t *testConfig) Teardown() error {
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

func setUpDefaultRouting() error {
	if err := applyRules("v1alpha3", defaultRules); err != nil {
		return fmt.Errorf("could not apply rules '%s': %v", defaultRules, err)
	}
	standby := 0
	for i := 0; i <= testRetryTimes; i++ {
		time.Sleep(time.Duration(standby) * time.Second)
		var gateway string
		var errGw error

		gateway, errGw = tc.Kube.IngressGateway()
		if errGw != nil {
			return errGw
		}

		resp, err := http.Get(fmt.Sprintf("%s/productpage", gateway))
		if err != nil {
			log.Infof("Error talking to productpage: %s", err)
		} else {
			log.Infof("Get from page: %d", resp.StatusCode)
			if resp.StatusCode == http.StatusOK {
				log.Info("Get response from product page!")
				break
			}
			closeResponseBody(resp)
		}
		if i == testRetryTimes {
			return errors.New("unable to set default route")
		}
		standby += 5
		log.Errorf("Couldn't get to the bookinfo product page, trying again in %d second", standby)
	}

	log.Info("Success! Default route got expected response")
	return nil
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
	defer closeResponseBody(resp)
	if resp.StatusCode != http.StatusOK {
		return -1, fmt.Errorf("status code is %d", resp.StatusCode)
	}
	duration := int(time.Since(startT) / (time.Second / time.Nanosecond))
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return -1, err
	}

	if err = util.CompareToFile(body, modelFile); err != nil {
		log.Errorf("Error: User %s in version %s didn't get expected response", user, version)
		duration = -1
	}
	return duration, err
}

func checkHTTPResponse(user, gateway, expr string, count int) (int, error) {
	resp, err := http.Get(fmt.Sprintf("%s/productpage", gateway))
	if err != nil {
		return -1, err
	}

	defer closeResponseBody(resp)
	log.Infof("Get from page: %d", resp.StatusCode)
	if resp.StatusCode != http.StatusOK {
		log.Errorf("Get response from product page failed!")
		return -1, fmt.Errorf("status code is %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return -1, err
	}

	if expr == "" {
		return 1, nil
	}

	re, err := regexp.Compile(expr)
	if err != nil {
		return -1, err
	}

	ref := re.FindAll(body, -1)
	if ref == nil {
		log.Infof("%v", string(body))
		return -1, fmt.Errorf("could not find %v in response", expr)
	}
	if count > 0 && len(ref) < count {
		log.Infof("%v", string(body))
		return -1, fmt.Errorf("could not find %v # of %v in response. found %v", count, expr, len(ref))
	}
	return 1, nil
}

func deleteRules(configVersion string, ruleKeys []string) error {
	var err error
	for _, ruleKey := range ruleKeys {
		rule := getPreprocessedRulePath(tc, configVersion, ruleKey)
		if e := util.KubeDelete(tc.Kube.Namespace, rule, tc.Kube.KubeConfig); e != nil {
			err = multierror.Append(err, e)
		}
	}
	log.Info("Waiting for rule to be cleaned up...")
	time.Sleep(time.Duration(30) * time.Second)
	return err
}

func applyRules(configVersion string, ruleKeys []string) error {
	for _, ruleKey := range ruleKeys {
		rule := getPreprocessedRulePath(tc, configVersion, ruleKey)
		if err := util.KubeApply(tc.Kube.Namespace, rule, tc.Kube.KubeConfig); err != nil {
			//log.Errorf("Kubectl apply %s failed", rule)
			return err
		}
	}
	log.Info("Waiting for rules to propagate...")
	time.Sleep(time.Duration(30) * time.Second)
	return nil
}

func getBookinfoResourcePath(resource string) string {
	return util.GetResourcePath(filepath.Join(bookinfoSampleDir, deploymentDir,
		resource+"."+yamlExtension))
}

func setTestConfig() error {
	cc, err := framework.NewCommonConfig("demo_test")
	if err != nil {
		return err
	}
	tc = new(testConfig)
	tc.CommonConfig = cc
	tc.rulesDir, err = ioutil.TempDir(os.TempDir(), "demo_test")
	if err != nil {
		return err
	}
	demoApps := []framework.App{{AppYaml: getBookinfoResourcePath(bookinfoYaml),
		KubeInject: true,
	},
		{AppYaml: getBookinfoResourcePath(bookinfoRatingsv2Yaml),
			KubeInject: true,
		},
		{AppYaml: getBookinfoResourcePath(bookinfoRatingsMysqlYaml),
			KubeInject: true,
		},
		{AppYaml: getBookinfoResourcePath(bookinfoDbYaml),
			KubeInject: true,
		},
		{AppYaml: getBookinfoResourcePath(bookinfoMysqlYaml),
			KubeInject: true,
		},
		{AppYaml: getBookinfoResourcePath(bookinfoDetailsExternalServiceYaml),
			KubeInject: true,
		},
	}
	for i := range demoApps {
		tc.Kube.AppManager.AddApp(&demoApps[i])
	}
	return nil
}

func TestMain(m *testing.M) {
	flag.Parse()
	check(framework.InitLogging(), "cannot setup logging")
	check(setTestConfig(), "could not create TestConfig")
	tc.Cleanup.RegisterCleanable(tc)
	os.Exit(tc.RunTest(m))
}

func getIngressOrFail(t *testing.T, configVersion string) string {
	return tc.Kube.IngressGatewayOrFail(t)
}
