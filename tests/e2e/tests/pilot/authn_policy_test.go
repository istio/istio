// Copyright 2018 Istio Authors
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

package pilot

import (
	"fmt"
	"io/ioutil"
	"testing"
)

func TestMTlsWithAuthNPolicy(t *testing.T) {
	// This policy will enable mTLS for all namespace, and disable mTLS for c and d:80.
	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{"testdata/authn/v1alpha1/authn-policy.yaml.tmpl", "testdata/authn/destination-rule.yaml.tmpl"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	srcPods := []string{"a", "t"}
	dstPods := []string{"b", "c", "d"}
	ports := []string{"", "80", "8080"}

	// Run all request tests.
	t.Run("request", func(t *testing.T) {
		for _, src := range srcPods {
			for _, dst := range dstPods {
				for _, port := range ports {
					for _, domain := range []string{"", "." + tc.Kube.Namespace} {
						testName := fmt.Sprintf("%s->%s%s_%s", src, dst, domain, port)
						runRetriableTest(t, testName, 15, func() error {
							reqURL := fmt.Sprintf("http://%s%s:%s/%s", dst, domain, port, src)
							resp := ClientRequest(src, reqURL, 1, "")
							if src == "t" && (dst == "b" || (dst == "d" && port == "8080")) {
								if len(resp.ID) == 0 {
									// t cannot talk to b nor d:8080
									return nil
								}
								return errAgain
							}
							// Request should return successfully (status 200)
							if resp.IsHTTPOk() {
								return nil
							}
							return errAgain
						})
					}
				}
			}
		}
	})
}

func TestAuthNJwt(t *testing.T) {
	// JWT token used is borrowed from https://github.com/istio/proxy/blob/master/src/envoy/http/jwt_auth/sample/correct_jwt.
	// The Token expires in year 2132, issuer is 628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com.
	// Test will fail if this service account is deleted.
	p := "testdata/authn/v1alpha1/correct_jwt"
	token, err := ioutil.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read %q", p)
	}
	validJwtToken := string(token)

	// Policy enforces JWT authn for service 'c' and 'd:80'.
	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{"testdata/authn/v1alpha1/authn-policy-jwt.yaml.tmpl"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if tc.Kube.AuthEnabled {
		cfgs.YamlFiles = append(cfgs.YamlFiles, "testdata/authn/destination-rule-authjwt.yaml.tmpl")
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	cases := []struct {
		dst    string
		src    string
		port   string
		token  string
		expect string
	}{
		{dst: "a", src: "b", port: "", token: "", expect: "200"},
		{dst: "a", src: "c", port: "80", token: "", expect: "200"},

		{dst: "b", src: "a", port: "", token: "", expect: "200"},
		{dst: "b", src: "a", port: "80", token: "", expect: "200"},
		{dst: "b", src: "c", port: "", token: validJwtToken, expect: "200"},
		{dst: "b", src: "d", port: "8080", token: "testToken", expect: "200"},

		{dst: "c", src: "a", port: "80", token: validJwtToken, expect: "200"},
		{dst: "c", src: "a", port: "8080", token: "invalidToken", expect: "401"},
		{dst: "c", src: "b", port: "", token: "random", expect: "401"},
		{dst: "c", src: "d", port: "80", token: validJwtToken, expect: "200"},

		{dst: "d", src: "a", port: "", token: validJwtToken, expect: "200"},
		{dst: "d", src: "b", port: "80", token: "foo", expect: "401"},
		{dst: "d", src: "c", port: "8080", token: "bar", expect: "200"},
	}

	for _, c := range cases {
		testName := fmt.Sprintf("%s->%s[%s]", c.src, c.dst, c.expect)
		runRetriableTest(t, testName, defaultRetryBudget, func() error {
			extra := fmt.Sprintf("-key \"Authorization\" -val \"Bearer %s\"", c.token)
			resp := ClientRequest(c.src, fmt.Sprintf("http://%s:%s", c.dst, c.port), 1, extra)
			if len(resp.Code) > 0 && resp.Code[0] == c.expect {
				return nil
			}

			return errAgain
		})
	}
}
