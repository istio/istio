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
	"fmt"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"istio.io/istio/pkg/log"
	"istio.io/istio/tests/e2e/framework"
	"istio.io/istio/tests/util"
)

type user struct {
	username      string
	sessionCookie string
}

type userVersion struct {
	user    user
	version string
	model   string
}

type versionRoutingRule struct {
	key          string
	userVersions []userVersion
}

func TestVersionRouting(t *testing.T) {
	v1Model := util.GetResourcePath(filepath.Join(modelDir, "productpage-normal-user-v1.html"))
	v2TestModel := util.GetResourcePath(filepath.Join(modelDir, "productpage-test-user-v2.html"))

	var rules = []versionRoutingRule{
		{key: testRule,
			userVersions: []userVersion{
				{
					user:    u1,
					version: "v1",
					model:   v1Model,
				},
				{
					user:    u2,
					version: "v2",
					model:   v2TestModel,
				},
			},
		},
	}

	testVersionRoutingRules(t, "v1alpha3", rules)
}

func testVersionRoutingRules(t *testing.T, configVersion string, rules []versionRoutingRule) {
	for _, rule := range rules {
		testVersionRoutingRule(t, configVersion, rule)
	}
}

func testVersionRoutingRule(t *testing.T, configVersion string, rule versionRoutingRule) {
	inspect(applyRules(configVersion, []string{rule.key}), "failed to apply rules", "", t)
	defer func() {
		inspect(deleteRules(configVersion, []string{rule.key}),
			fmt.Sprintf("failed to delete rules"), "", t)
		inspect(applyRules(configVersion, defaultRules), "failed to apply rules", "", t)
	}()

	for _, userVersion := range rule.userVersions {
		_, err := checkRoutingResponse(userVersion.user, userVersion.version,
			getIngressOrFail(t, configVersion), userVersion.model)
		inspect(
			err, fmt.Sprintf("Failed version routing! %s in %s", userVersion.user, userVersion.version),
			fmt.Sprintf("Success! Response matches with expected! %s in %s", userVersion.user,
				userVersion.version), t)
	}
}

func TestFaultDelay(t *testing.T) {
	var rules = []string{testRule, delayRule}
	doTestFaultDelay(t, "v1alpha3", rules)
}

func doTestFaultDelay(t *testing.T, configVersion string, rules []string) {
	inspect(applyRules(configVersion, rules), "failed to apply rules", "", t)
	defer func() {
		inspect(deleteRules(configVersion, rules), "failed to delete rules", "", t)
		inspect(applyRules(configVersion, defaultRules), "failed to apply rules", "", t)
	}()
	minDuration := 5
	maxDuration := 8
	standby := 10
	testModel := util.GetResourcePath(
		filepath.Join(modelDir, "productpage-test-user-v1-review-timeout.html"))
	for i := 0; i < testRetryTimes; i++ {
		duration, err := checkRoutingResponse(
			u2, "v1-timeout", getIngressOrFail(t, configVersion),
			testModel)
		log.Infof("Get response in %d second", duration)
		if err == nil && duration >= minDuration && duration <= maxDuration {
			log.Info("Success! Fault delay as expected")
			break
		}

		if i == testRetryTimes-1 {
			t.Errorf("Fault delay failed! Delay in %ds while expected between %ds and %ds, %s",
				duration, minDuration, maxDuration, err)
			break
		}

		log.Infof("Unexpected response, retry in %ds", standby)
		time.Sleep(time.Duration(standby) * time.Second)
	}
}

type migrationRule struct {
	key            string
	rate           float64
	modelToMigrate string
}

func TestVersionMigration(t *testing.T) {
	doTestVersionMigration(t, "v1alpha3")
}

func doTestVersionMigration(t *testing.T, configVersion string) {
	modelV2 := util.GetResourcePath(filepath.Join(modelDir, "productpage-normal-user-v2.html"))
	modelV3 := util.GetResourcePath(filepath.Join(modelDir, "productpage-normal-user-v3.html"))

	var rules = []migrationRule{
		{
			key:            fiftyRule,
			modelToMigrate: modelV3,
			rate:           0.5,
		},
		{
			key:            twentyRule,
			modelToMigrate: modelV2,
			rate:           0.2,
		},
		{
			key:            tenRule,
			modelToMigrate: modelV2,
			rate:           0.1,
		},
	}

	for _, rule := range rules {
		testVersionMigrationRule(t, configVersion, rule)
	}
}

func testVersionMigrationRule(t *testing.T, configVersion string, rule migrationRule) {
	inspect(applyRules(configVersion, []string{rule.key}), "failed to apply rules", "", t)
	defer func() {
		inspect(deleteRules(configVersion, []string{rule.key}),
			fmt.Sprintf("failed to delete rules"), "", t)
		inspect(applyRules(configVersion, defaultRules), "failed to apply rules", "", t)
	}()
	modelV1 := util.GetResourcePath(filepath.Join(modelDir, "productpage-normal-user-v1.html"))
	tolerance := 0.05
	totalShot := 100
	cookies := []http.Cookie{
		{
			Name:  "foo",
			Value: "bar",
		},
		{
			Name:  "session",
			Value: u1.sessionCookie,
		},
	}

	for i := 0; i < testRetryTimes; i++ {
		c1, cVersionToMigrate := 0, 0
		for c := 0; c < totalShot; c++ {
			resp, err := getWithCookie(fmt.Sprintf("%s/productpage",
				getIngressOrFail(t, configVersion)), cookies)
			inspect(err, "Failed to record", "", t)
			if resp.StatusCode != http.StatusOK {
				log.Errorf("unexpected response status %d", resp.StatusCode)
				continue
			}
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Errora(err)
				continue
			}

			var c1CompareError, cVersionToMigrateError error
			if c1CompareError = util.CompareToFile(body, modelV1); c1CompareError == nil {
				c1++
			} else if cVersionToMigrateError = util.CompareToFile(body, rule.modelToMigrate); cVersionToMigrateError == nil {
				cVersionToMigrate++
			} else {
				log.Error("received unexpected version: %s")
				log.Infof("comparing to the original version: %v", c1CompareError)
				log.Infof("comparing to the version to migrate to: %v", cVersionToMigrateError)
			}
			closeResponseBody(resp)
		}

		if isWithinPercentage(c1, totalShot, 1.0-rule.rate, tolerance) &&
			isWithinPercentage(cVersionToMigrate, totalShot, rule.rate, tolerance) {
			log.Infof(
				"Success! Version migration acts as expected for rate %f, "+
					"old version hit %d, new version hit %d", rule.rate, c1, cVersionToMigrate)
			break
		}

		if i == testRetryTimes-1 {
			t.Errorf("Failed version migration test for rate %f, "+
				"old version hit %d, new version hit %d", rule.rate, c1, cVersionToMigrate)
		}
	}
}

func isWithinPercentage(count int, total int, rate float64, tolerance float64) bool {
	minimum := int((rate - tolerance) * float64(total))
	maximum := int((rate + tolerance) * float64(total))
	return count >= minimum && count <= maximum
}

func TestDbRoutingMongo(t *testing.T) {
	var rules = []string{testDbRule}
	doTestDbRoutingMongo(t, "v1alpha3", rules)
}

func doTestDbRoutingMongo(t *testing.T, configVersion string, rules []string) {
	var err error
	inspect(applyRules(configVersion, rules), "failed to apply rules", "", t)
	defer func() {
		inspect(deleteRules(configVersion, rules), "failed to delete rules", "", t)
		inspect(applyRules(configVersion, defaultRules), "failed to apply rules", "", t)
	}()

	// TODO: update the rating in the db and check the value on page

	respExpr := "glyphicon-star" // not great test for v2 or v3 being alive

	_, err = checkHTTPResponse(getIngressOrFail(t, configVersion), respExpr, 10)
	inspect(
		err, fmt.Sprintf("Failed database routing! %s in v1", u1),
		fmt.Sprintf("Success! Response matches with expected! %s", respExpr), t)
}

func TestDbRoutingMysql(t *testing.T) {
	var rules = []string{testMysqlRule}

	doTestDbRoutingMysql(t, "v1alpha3", rules)
}

func doTestDbRoutingMysql(t *testing.T, configVersion string, rules []string) {
	var err error
	inspect(applyRules(configVersion, rules), "failed to apply rules", "", t)
	defer func() {
		inspect(deleteRules(configVersion, rules), "failed to delete rules", "", t)
		inspect(applyRules(configVersion, defaultRules), "failed to apply rules", "", t)
	}()

	// TODO: update the rating in the db and check the value on page

	respExpr := "glyphicon-star" // not great test for v2 or v3 being alive

	_, err = checkHTTPResponse(getIngressOrFail(t, configVersion), respExpr, 10)
	inspect(
		err, fmt.Sprintf("Failed database routing! %s in v1", u1),
		fmt.Sprintf("Success! Response matches with expected! %s", respExpr), t)
}

func TestVMExtendsIstio(t *testing.T) {
	t.Skip("issue https://github.com/istio/istio/issues/4794")
	if *framework.TestVM {
		// TODO (chx) vm_provider flag to select venders
		vm, err := framework.NewGCPRawVM(tc.CommonConfig.Kube.Namespace)
		inspect(err, "unable to configure VM", "VM configured correctly", t)
		// VM setup and teardown is manual for now
		// will be replaced with preprovision server calls
		err = vm.Setup()
		inspect(err, "VM setup failed", "VM setup succeeded", t)
		_, err = vm.SecureShell("curl -v istio-pilot:8080")
		inspect(err, "VM failed to extend istio", "VM extends istio service mesh", t)
		_, err2 := vm.SecureShell(fmt.Sprintf(
			"host istio-pilot.%s.svc.cluster.local.", vm.Namespace))
		inspect(err2, "VM failed to extend istio", "VM extends istio service mesh", t)
		err = vm.Teardown()
		inspect(err, "VM teardown failed", "VM teardown succeeded", t)
	}
}

func TestExternalDetailsService(t *testing.T) {
	if !tf.Egress {
		t.Skipf("Skipping %s: egress=false", t.Name())
	}

	var rules = []string{detailsExternalServiceRouteRule, detailsExternalServiceEgressRule}

	doTestExternalDetailsService(t, "v1alpha3", rules)
}

func doTestExternalDetailsService(t *testing.T, configVersion string, rules []string) {
	var err error
	inspect(applyRules(configVersion, rules), "failed to apply rules", "", t)
	defer func() {
		inspect(deleteRules(configVersion, rules), "failed to delete rules", "", t)
		inspect(applyRules(configVersion, defaultRules), "failed to apply rules", "", t)
	}()

	isbnFetchedFromExternalService := "0486424618"

	_, err = checkHTTPResponse(getIngressOrFail(t, configVersion), isbnFetchedFromExternalService, 1)
	inspect(
		err, fmt.Sprintf("Failed external details routing! %s in v1", u1),
		fmt.Sprintf("Success! Response matches with expected! %s", isbnFetchedFromExternalService), t)

	// Temp: verify the status of pilot, this test fails only on prow.
	util.FetchAndSaveClusterLogs(tc.Kube.Namespace, tc.Info.TempDir + "/afterExternal", tc.Kube.KubeConfig)

}
