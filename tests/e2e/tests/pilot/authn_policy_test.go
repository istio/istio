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

func TestAuthNPolicy(t *testing.T) {
	if !tc.Kube.AuthEnabled {
		t.Skipf("Skipping %s: auth_enable=false", t.Name())
	}

	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{"testdata/authn/v1alpha1/authn-policy.yaml.tmpl"},
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
						runRetriableTest(t, testName, defaultRetryBudget, func() error {
							reqURL := fmt.Sprintf("http://%s%s:%s/%s", dst, domain, port, src)
							resp := ClientRequest(src, reqURL, 1, "")
							if src == "t" && (dst == "b" || (dst == "d" && port == "8080")) {
								if len(resp.ID) == 0 {
									// t cannot talk to b nor d:80
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
	// V1alpha3 == true implies envoyv2, jwt authn doesn't work for v2 so skip it now.
	if tc.V1alpha3 {
		t.Skipf("Skipping %s: V1alpha3=true", t.Name())
	}

	// JWT token used is borrowed from https://github.com/istio/proxy/blob/master/src/envoy/http/jwt_auth/sample/correct_jwt.
	// The Token expires in year 2132, issuer is 628645741881-noabiu23f5a8m8ovd8ucv698lj78vv0l@developer.gserviceaccount.com.
	// Test will fail if this service account is deleted.
	p := "testdata/authn/v1alpha1/correct_jwt"
	token, err := ioutil.ReadFile(p)
	if err != nil {
		t.Fatalf("failed to read %q", p)
	}
	validJwtToken := string(token)

	cfgs := &deployableConfig{
		Namespace:  tc.Kube.Namespace,
		YamlFiles:  []string{"testdata/authn/v1alpha1/authn-policy-jwt.yaml.tmpl"},
		kubeconfig: tc.Kube.KubeConfig,
	}
	if err := cfgs.Setup(); err != nil {
		t.Fatal(err)
	}
	defer cfgs.Teardown()

	cases := []struct {
		dst          string
		src          string
		path         string
		port         string
		validToken   string
		invalidToken string
		expect       string
	}{
		{dst: "a", src: "b", path: "/", port: "", validToken: "", invalidToken: "", expect: "200"},
		{dst: "a", src: "c", path: "/xyz", port: "80", validToken: "", invalidToken: "", expect: "200"},

		{dst: "b", src: "a", path: "/", port: "", validToken: "", invalidToken: "", expect: "200"},
		{dst: "b", src: "a", path: "/xyz", port: "80", validToken: "", invalidToken: "", expect: "200"},
		{dst: "b", src: "c", path: "/", port: "", validToken: validJwtToken, invalidToken: "", expect: "200"},
		{dst: "b", src: "d", path: "/xyz", port: "8080", validToken: "", invalidToken: "testToken", expect: "200"},

		{dst: "c", src: "a", path: "/", port: "80", validToken: validJwtToken, invalidToken: "", expect: "200"},
		{dst: "c", src: "a", path: "/xyz", port: "8080", validToken: "", invalidToken: "invalidToken", expect: "401"},
		{dst: "c", src: "b", path: "/test", port: "", validToken: "", invalidToken: "random", expect: "401"},
		{dst: "c", src: "d", path: "/prefix", port: "80", validToken: validJwtToken, invalidToken: "", expect: "200"},

		{dst: "d", src: "a", path: "/xyz", port: "", validToken: validJwtToken, invalidToken: "", expect: "200"},
		{dst: "d", src: "b", path: "/", port: "80", validToken: "", invalidToken: "foo", expect: "401"},
		{dst: "d", src: "c", path: "/", port: "8080", validToken: "", invalidToken: "bar", expect: "401"},
	}

	for _, c := range cases {
		testName := fmt.Sprintf("%s->%s%s[%s]", c.src, c.dst, c.path, c.expect)
		runRetriableTest(t, testName, defaultRetryBudget, func() error {
			extra := ""
			if c.validToken != "" {
				extra = fmt.Sprintf("-key \"Authorization\" -val \"Bearer %s\"", c.validToken)
			} else if c.invalidToken != "" {
				extra = fmt.Sprintf("-key \"Authorization\" -val \"Bearer %s\"", c.invalidToken)
			}

			resp := ClientRequest(c.src, fmt.Sprintf("http://%s%s", c.dst, c.path), 1, extra)
			if len(resp.Code) > 0 && resp.Code[0] == c.expect {
				return nil
			}

			return errAgain
		})
	}
}
